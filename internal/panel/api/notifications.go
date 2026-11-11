package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
)

// Custom notifications: rules that send a message in your own words to an alert channel when something
// happens to a game server (a backup made or failed, the game starting, stopping or crashing), for every
// game or one. A message about a backup made can carry a link that downloads it without signing in, for
// a few days: only a hash of the link is kept, and it stops working when it expires, is revoked, or the
// backup is deleted.

var notificationEvents = map[string]struct{ title, message string }{
	"backup_done":   {"{game}: backed up", "Backup \"{backup}\" of {game} on {server} is done ({size}).\n{link}"},
	"backup_failed": {"{game}: backup failed", "Backup \"{backup}\" of {game} on {server} failed: {error}"},
	"game_started":  {"{game} is up", "{game} on {server} has started."},
	"game_stopped":  {"{game} stopped", "{game} on {server} has stopped."},
	"game_crashed":  {"{game} crashed", "{game} on {server} stopped by itself (it crashed)."},
}

const maxLinkDays = 30

type notificationRule struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Event     string `json:"event"`
	GameID    int64  `json:"gameId"` // 0 for any game
	ChannelID int64  `json:"channelId"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	LinkDays  int    `json:"linkDays"`
	Enabled   bool   `json:"enabled"`
}

func (api *API) notificationRoutes() {
	api.gameEvents = &gameStates{last: map[int64]string{}}
	m := api.mux
	m.Handle("GET /api/notifications", api.owner(api.listRules))
	m.Handle("POST /api/notifications", api.owner(api.saveRule))
	m.Handle("PUT /api/notifications/{id}", api.owner(api.saveRule))
	m.Handle("DELETE /api/notifications/{id}", api.owner(api.deleteRule))
	m.Handle("POST /api/notifications/{id}/test", api.owner(api.testRule))
	m.Handle("DELETE /api/games/{id}/backups/{backup}/links", api.gameOwner(api.revokeBackupLinks))
	// Signed out on purpose: the link itself is what lets it be used.
	m.HandleFunc("GET /api/backup-links/{token}", api.downloadByLink)
}

const ruleColumns = `id, name, event, coalesce(game_id, 0), channel_id, title, message, link_days, enabled`

func scanRule(row interface{ Scan(...any) error }) (notificationRule, error) {
	var n notificationRule
	err := row.Scan(&n.ID, &n.Name, &n.Event, &n.GameID, &n.ChannelID, &n.Title, &n.Message, &n.LinkDays, &n.Enabled)
	return n, err
}

func (api *API) listRules(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT `+ruleColumns+` FROM notification_rules ORDER BY name`)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []notificationRule{}
	for rows.Next() {
		if n, err := scanRule(rows); err == nil {
			list = append(list, n)
		}
	}
	writeJSON(w, list)
}

func (api *API) saveRule(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var n notificationRule
	if !readJSON(w, r, &n) {
		return
	}
	n.Name, n.Title, n.Message = strings.TrimSpace(n.Name), strings.TrimSpace(n.Title), strings.TrimSpace(n.Message)
	_, known := notificationEvents[n.Event]
	var channels, games int
	_ = api.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM alert_channels WHERE id = ?`, n.ChannelID).Scan(&channels)
	_ = api.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM games WHERE id = ?`, n.GameID).Scan(&games)
	switch {
	case n.Name == "" || len(n.Name) > 80:
		writeError(w, http.StatusBadRequest, "give it a name, up to 80 letters")
	case !known:
		writeError(w, http.StatusBadRequest, "say when it's sent")
	case n.GameID != 0 && games == 0:
		writeError(w, http.StatusBadRequest, "there's no such game server")
	case channels == 0:
		writeError(w, http.StatusBadRequest, "say where it's sent (add a channel first)")
	case n.Title == "" || len(n.Title) > 200 || n.Message == "" || len(n.Message) > 2000:
		writeError(w, http.StatusBadRequest, "it needs a title (up to 200 letters) and a message (up to 2,000)")
	case n.LinkDays < 0 || n.LinkDays > maxLinkDays || (n.LinkDays > 0 && n.Event != "backup_done"):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a download link is for a backup made, and works for 1 to %d days", maxLinkDays))
	default:
		game := sql.NullInt64{Int64: n.GameID, Valid: n.GameID != 0}
		var err error
		if r.Method == http.MethodPost {
			_, err = api.db.ExecContext(r.Context(), `INSERT INTO notification_rules (name, event, game_id, channel_id, title, message, link_days, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				n.Name, n.Event, game, n.ChannelID, n.Title, n.Message, n.LinkDays, n.Enabled)
		} else {
			_, err = api.db.ExecContext(r.Context(), `UPDATE notification_rules SET name = ?, event = ?, game_id = ?, channel_id = ?, title = ?, message = ?, link_days = ?, enabled = ? WHERE id = ?`,
				n.Name, n.Event, game, n.ChannelID, n.Title, n.Message, n.LinkDays, n.Enabled, r.PathValue("id"))
		}
		if err != nil {
			api.fail(w, r, err)
			return
		}
		api.log.Info("notification saved", "user", u.Username, "name", n.Name, "event", n.Event)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (api *API) deleteRule(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM notification_rules WHERE id = ?`, r.PathValue("id")); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("notification deleted", "user", u.Username)
	w.WriteHeader(http.StatusNoContent)
}

// testRule sends the notification now, with made-up details (and no working link).
func (api *API) testRule(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	n, err := scanRule(api.db.QueryRowContext(r.Context(), `SELECT `+ruleColumns+` FROM notification_rules WHERE id = ?`, r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such notification")
		return
	}
	values := map[string]string{"game": "Survival SMP", "server": "atlas", "backup": "Nightly", "size": "4.2 GB", "error": "the disk is full",
		"time": time.Now().Format("15:04 on 2 Jan"), "link": ""}
	if n.LinkDays > 0 {
		values["link"] = api.cfg.PublicURL.String() + "/api/backup-links/a-test-link-that-does-not-work"
	}
	if err := api.sendRule(r.Context(), n, values); err != nil {
		writeError(w, http.StatusBadGateway, "it couldn't be sent: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fill puts the values in a title or message: {game}, {server}, {backup}, {size}, {error}, {time}, {link}.
func fill(text string, values map[string]string) string {
	pairs := make([]string, 0, len(values)*2)
	for k, v := range values {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.TrimSpace(strings.NewReplacer(pairs...).Replace(text))
}

func (api *API) sendRule(ctx context.Context, n notificationRule, values map[string]string) error {
	channels, err := api.channels(ctx)
	if err != nil {
		return err
	}
	for _, c := range channels {
		if c.ID == n.ChannelID {
			body := fill(n.Message, values)
			if link := values["link"]; link != "" && !strings.Contains(n.Message, "{link}") {
				body += "\n" + link // asked for a link but the message doesn't say where: at the end
			}
			return api.sendLinked(ctx, c, fill(n.Title, values), body, values["link"])
		}
	}
	return errors.New("its channel has gone")
}

// notify sends every enabled notification for event on g. For a backup made, backupID is the backup, for
// any download links. It runs in the background: a slow channel holds nothing up.
func (api *API) notify(event string, g *game, backupID int64, values map[string]string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rows, err := api.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE enabled = 1 AND event = ? AND (game_id IS NULL OR game_id = ?)`, event, g.ID)
		if err != nil {
			return
		}
		var rules []notificationRule
		for rows.Next() {
			if n, err := scanRule(rows); err == nil {
				rules = append(rules, n)
			}
		}
		rows.Close()
		if len(rules) == 0 {
			return
		}
		var server string
		_ = api.db.QueryRowContext(ctx, `SELECT name FROM servers WHERE id = ?`, g.ServerID).Scan(&server)
		base := map[string]string{"game": g.Name, "server": server, "time": time.Now().Format("15:04 on 2 Jan"), "backup": "", "size": "", "error": "", "link": ""}
		for k, v := range values {
			base[k] = v
		}
		for _, n := range rules {
			v := base
			if n.LinkDays > 0 && backupID > 0 {
				v = map[string]string{}
				for k, x := range base {
					v[k] = x
				}
				link, err := api.newBackupLink(ctx, backupID, n.LinkDays)
				if err != nil {
					api.log.Warn("a download link couldn't be made", "err", err)
				}
				v["link"] = link
			}
			if err := api.sendRule(ctx, n, v); err != nil {
				api.log.Warn("sending a notification", "notification", n.Name, "error", err)
			}
		}
	}()
}

// newBackupLink makes a link that downloads a backup for days, keeping only its hash.
func (api *API) newBackupLink(ctx context.Context, backupID int64, days int) (string, error) {
	token := auth.RandomToken()
	expires := time.Now().UTC().AddDate(0, 0, days).Format(time.RFC3339)
	if _, err := api.db.ExecContext(ctx, `INSERT INTO backup_links (backup_id, token_hash, expires_at) VALUES (?, ?, ?)`, backupID, hashLink(token), expires); err != nil {
		return "", err
	}
	return api.cfg.PublicURL.String() + "/api/backup-links/" + token, nil
}

func hashLink(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// downloadByLink sends the backup a link is for, if it's still good. Wrong links count towards the
// sign-in limits for that address, so they can't be guessed at speed (they're 256-bit and random anyway).
func (api *API) downloadByLink(w http.ResponseWriter, r *http.Request) {
	address := api.clientAddress(r)
	if wait := api.auth.Limiter.Wait("", address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	var backupID, gameID int64
	err := api.db.QueryRowContext(r.Context(), `SELECT l.backup_id, b.game_id FROM backup_links l JOIN game_backups b ON b.id = l.backup_id
		WHERE l.token_hash = ? AND l.expires_at > ? AND b.state = 'done'`, hashLink(r.PathValue("token")), time.Now().UTC().Format(time.RFC3339)).Scan(&backupID, &gameID)
	if err != nil {
		api.auth.Limiter.Failed("", address)
		writeError(w, http.StatusNotFound, "this link has expired or been stopped, or its backup deleted")
		return
	}
	g, err := api.loadGame(r.Context(), gameID)
	if err != nil {
		writeError(w, http.StatusNotFound, "this link's game server has been deleted")
		return
	}
	b, err := api.backup(r.Context(), g, backupID)
	if err != nil {
		writeError(w, http.StatusNotFound, "this link's backup has been deleted")
		return
	}
	api.log.Info("backup downloaded by link", "game", g.ID, "backup", b.Name, "address", address)
	api.sendBackup(w, r, g, b)
}

func (api *API) revokeBackupLinks(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM backup_links WHERE backup_id IN (SELECT id FROM game_backups WHERE id = ? AND game_id = ?)`, id, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("backup links stopped", "user", u.Username, "game", g.ID, "backup", id)
	w.WriteHeader(http.StatusNoContent)
}

// Games' states as last seen, so a change can be told: started, stopped or crashed.
type gameStates struct {
	mu   sync.Mutex
	last map[int64]string
}

// checkGameEvents looks at every game's state and sends the notifications for any that changed. Run with
// the alert checks, so a change is told within half a minute.
func (api *API) checkGameEvents(ctx context.Context) {
	servers, err := api.hub.Servers(ctx)
	if err != nil {
		return
	}
	now := map[int64]string{}
	for _, s := range servers {
		live := api.hub.Live(s.ID)
		if !live.Online || live.Metrics == nil {
			continue // not known: an offline server's games are neither started nor stopped
		}
		for _, gs := range live.Metrics.Games {
			now[gs.ID] = gs.State
		}
	}
	api.gameEvents.mu.Lock()
	changed := map[int64][2]string{}
	for id, state := range now {
		if before, seen := api.gameEvents.last[id]; seen && before != state {
			changed[id] = [2]string{before, state}
		}
		api.gameEvents.last[id] = state
	}
	api.gameEvents.mu.Unlock()
	for id, c := range changed {
		event := gameEvent(c[0], c[1])
		if event == "" {
			continue
		}
		if g, err := api.loadGame(ctx, id); err == nil {
			api.notify(event, g, 0, nil)
		}
	}
}

// gameEvent is what a change of state counts as, if anything.
func gameEvent(before, after string) string {
	up := func(s string) bool { return s == "running" || s == "starting" }
	switch {
	case after == "running" && before != "running":
		return "game_started"
	case after == "crashed" && before != "crashed":
		return "game_crashed"
	case after == "stopped" && up(before):
		return "game_stopped"
	}
	return ""
}
