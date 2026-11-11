package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/cron"
	"github.com/Atrello/bastion/internal/protocol"
)

// Game schedules: a restart (warning players a minute before), a console command, or a backup, at times
// given as a cron expression. The panel runs them, so they run whichever server the game is on. The same
// minute's check stops games that have gone over their disk limit.

func (api *API) scheduleRoutes() {
	m := api.mux
	m.Handle("GET /api/games/{id}/schedules", api.gameUser(api.listSchedules))
	m.Handle("POST /api/games/{id}/schedules", api.gameUser(api.saveSchedule))
	m.Handle("PUT /api/games/{id}/schedules/{schedule}", api.gameUser(api.saveSchedule))
	m.Handle("DELETE /api/games/{id}/schedules/{schedule}", api.gameUser(api.deleteSchedule))
	m.Handle("POST /api/games/{id}/schedules/{schedule}/run", api.gameUser(api.runScheduleNow))
}

type schedule struct {
	ID        int64  `json:"id"`
	GameID    int64  `json:"-"`
	Name      string `json:"name"`
	Cron      string `json:"when"`
	Action    string `json:"action"`
	Payload   string `json:"payload"`
	Enabled   bool   `json:"enabled"`
	LastRunAt string `json:"lastRunAt"`
	LastError string `json:"lastError"`
	NextRun   string `json:"nextRun"`
}

const scheduleColumns = `id, game_id, name, cron, action, payload, enabled, COALESCE(last_run_at, ''), last_error`

func (api *API) listSchedules(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT `+scheduleColumns+` FROM game_schedules WHERE game_id = ? ORDER BY id`, g.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []schedule{}
	for rows.Next() {
		var s schedule
		if rows.Scan(&s.ID, &s.GameID, &s.Name, &s.Cron, &s.Action, &s.Payload, &s.Enabled, &s.LastRunAt, &s.LastError) == nil {
			if c, err := cron.Parse(s.Cron); err == nil && s.Enabled {
				s.NextRun = c.Next(time.Now()).UTC().Format(time.RFC3339)
			}
			list = append(list, s)
		}
	}
	writeJSON(w, list)
}

func (api *API) saveSchedule(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var s schedule
	if !readJSON(w, r, &s) {
		return
	}
	s.Name, s.Cron, s.Payload = strings.TrimSpace(s.Name), strings.TrimSpace(s.Cron), strings.TrimSpace(s.Payload)
	if _, err := cron.Parse(s.Cron); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch {
	case s.Name == "" || len(s.Name) > 60:
		writeError(w, http.StatusBadRequest, "give it a name")
		return
	case s.Action != "restart" && s.Action != "command" && s.Action != "backup":
		writeError(w, http.StatusBadRequest, "a schedule restarts, runs a command or backs up")
		return
	case s.Action == "command" && (s.Payload == "" || strings.ContainsAny(s.Payload, "\r\n") || len(s.Payload) > 1000):
		writeError(w, http.StatusBadRequest, "the command is one line")
		return
	}
	var err error
	if id := r.PathValue("schedule"); id != "" {
		_, err = api.db.ExecContext(r.Context(), `UPDATE game_schedules SET name = ?, cron = ?, action = ?, payload = ?, enabled = ? WHERE id = ? AND game_id = ?`,
			s.Name, s.Cron, s.Action, s.Payload, s.Enabled, id, g.ID)
	} else {
		_, err = api.db.ExecContext(r.Context(), `INSERT INTO game_schedules (game_id, name, cron, action, payload, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
			g.ID, s.Name, s.Cron, s.Action, s.Payload, s.Enabled)
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("schedule saved", "user", u.Username, "game", g.ID, "schedule", s.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteSchedule(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM game_schedules WHERE id = ? AND game_id = ?`, r.PathValue("schedule"), g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("schedule deleted", "user", u.Username, "game", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) runScheduleNow(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("schedule"), 10, 64)
	var s schedule
	err := api.db.QueryRowContext(r.Context(), `SELECT `+scheduleColumns+` FROM game_schedules WHERE id = ? AND game_id = ?`, id, g.ID).
		Scan(&s.ID, &s.GameID, &s.Name, &s.Cron, &s.Action, &s.Payload, &s.Enabled, &s.LastRunAt, &s.LastError)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such schedule")
		return
	}
	if err := api.runSchedule(r.Context(), s); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("schedule run", "user", u.Username, "game", g.ID, "schedule", s.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) runSchedule(ctx context.Context, s schedule) error {
	g, err := api.loadGame(ctx, s.GameID)
	if err != nil {
		return errors.New("no such game server")
	}
	switch s.Action {
	case "restart":
		err = api.power(ctx, g, "restart", 60)
	case "command":
		err = api.hub.Request(ctx, g.ServerID, protocol.OpGamePower, protocol.GamePowerArgs{ID: g.ID, Action: "command", Command: s.Payload}, nil)
	case "backup":
		_, err = api.startBackup(ctx, g, s.Name)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	_, _ = api.db.ExecContext(ctx, `UPDATE game_schedules SET last_run_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), last_error = ? WHERE id = ?`, message, s.ID)
	return err
}

// runSchedules checks at the start of every minute for schedules due, and for games over their disk.
func (api *API) runSchedules(ctx context.Context) {
	for {
		now := time.Now()
		select {
		case <-ctx.Done():
			return
		case <-time.After(now.Truncate(time.Minute).Add(time.Minute).Sub(now)):
		}
		minute := time.Now().Truncate(time.Minute)
		rows, err := api.db.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM game_schedules WHERE enabled = 1`)
		if err != nil {
			continue
		}
		var due []schedule
		for rows.Next() {
			var s schedule
			if rows.Scan(&s.ID, &s.GameID, &s.Name, &s.Cron, &s.Action, &s.Payload, &s.Enabled, &s.LastRunAt, &s.LastError) != nil {
				continue
			}
			if c, err := cron.Parse(s.Cron); err == nil && c.Matches(minute) {
				due = append(due, s)
			}
		}
		rows.Close()
		for _, s := range due {
			go func() {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				if err := api.runSchedule(ctx, s); err != nil {
					api.log.Warn("schedule failed", "game", s.GameID, "schedule", s.Name, "err", err)
				}
			}()
		}
		api.enforceDisk(ctx)
	}
}

// enforceDisk stops running games that are over their disk limit: Docker can't cap a folder's size, so
// the panel watches it instead.
func (api *API) enforceDisk(ctx context.Context) {
	rows, err := api.db.QueryContext(ctx, `SELECT `+gameColumns+` FROM games g JOIN eggs e ON e.id = g.egg_id WHERE g.disk_mb > 0`)
	if err != nil {
		return
	}
	var over []*game
	for rows.Next() {
		if g, err := scanGame(rows); err == nil {
			if v := api.view(g, "", false); (v.State == "running" || v.State == "starting") && v.DiskUsed > v.DiskLimit {
				over = append(over, g)
			}
		}
	}
	rows.Close()
	for _, g := range over {
		api.log.Warn("game over its disk limit: stopping it", "game", g.ID, "limit_mb", g.DiskMB)
		_ = api.power(ctx, g, "stop", 30)
	}
}
