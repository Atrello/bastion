package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
)

// The activity log: who did what, for the owner to read. Every change in the panel already writes a log
// line saying who made it (api.log.Info("game server deleted", "user", ..., "game", ...)); a line that
// names a user is kept here too, so nothing has to remember to record twice. A server or game given by
// its number is kept by its name, so the entry still reads right after it's deleted. Entries last 90 days.

const activityKept = 90 * 24 * time.Hour

// activityHandler passes every log line on, and keeps those that name a user. Keeping them is done in the
// background, by one writer: the panel has one database connection, and a log line written while it's in
// use (in the middle of reading rows, say) must never wait for it.
type activityHandler struct {
	slog.Handler
	db     *sql.DB
	writes chan slog.Record
}

func newActivityHandler(next slog.Handler, db *sql.DB) *activityHandler {
	h := &activityHandler{Handler: next, db: db, writes: make(chan slog.Record, 1000)}
	go func() {
		for rec := range h.writes {
			h.keep(rec)
		}
	}()
	return h
}

func (h *activityHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level == slog.LevelInfo && namesUser(rec) {
		select {
		case h.writes <- rec.Clone():
		default: // a thousand waiting: the database is stuck, and the log line is still in the log
		}
	}
	return h.Handler.Handle(ctx, rec)
}

func namesUser(rec slog.Record) bool {
	found := false
	rec.Attrs(func(a slog.Attr) bool {
		found = a.Key == "user"
		return !found
	})
	return found
}

func (h *activityHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &activityHandler{Handler: h.Handler.WithAttrs(attrs), db: h.db, writes: h.writes}
}

func (h *activityHandler) WithGroup(name string) slog.Handler {
	return &activityHandler{Handler: h.Handler.WithGroup(name), db: h.db, writes: h.writes}
}

func (h *activityHandler) keep(rec slog.Record) {
	var user string
	ok := true
	details := map[string]string{}
	var order []string
	rec.Attrs(func(a slog.Attr) bool {
		value := a.Value.Resolve()
		switch a.Key {
		case "user":
			user = value.String()
		case "ok":
			ok = value.Kind() != slog.KindBool || value.Bool()
		default:
			text := value.String()
			// A server or game by its number: kept by name.
			if value.Kind() == slog.KindInt64 && (a.Key == "server" || a.Key == "game") {
				table := map[string]string{"server": "servers", "game": "games"}[a.Key]
				var name string
				if h.db.QueryRow(`SELECT name FROM `+table+` WHERE id = ?`, value.Int64()).Scan(&name) == nil {
					text = name
				}
			}
			if len(text) > 300 {
				text = text[:300] + "…"
			}
			details[a.Key] = text
			order = append(order, a.Key)
		}
		return true
	})
	if user == "" {
		return
	}
	// As a JSON object in the order given, which reads better than sorted.
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		val, _ := json.Marshal(details[k])
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	_, _ = h.db.Exec(`INSERT INTO activity (user, action, details, ok) VALUES (?, ?, ?, ?)`, user, rec.Message, b.String(), ok)
}

type activityEntry struct {
	ID      int64           `json:"id"`
	At      string          `json:"at"`
	User    string          `json:"user"`
	Action  string          `json:"action"`
	Details json.RawMessage `json:"details"`
	OK      bool            `json:"ok"`
}

func (api *API) activityRoutes() {
	api.mux.Handle("GET /api/activity", api.owner(api.listActivity))
}

// listActivity gives the newest entries first, 100 at a time: ?before=<id> for older ones, ?user= for one
// person's, ?q= for those mentioning some text.
func (api *API) listActivity(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	q := r.URL.Query()
	where, args := []string{"1 = 1"}, []any{}
	if before, _ := strconv.ParseInt(q.Get("before"), 10, 64); before > 0 {
		where, args = append(where, "id < ?"), append(args, before)
	}
	if user := q.Get("user"); user != "" {
		where, args = append(where, "user = ?"), append(args, user)
	}
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(text) + "%"
		where, args = append(where, `(action LIKE ? ESCAPE '\' OR details LIKE ? ESCAPE '\' OR user LIKE ? ESCAPE '\')`), append(args, like, like, like)
	}
	rows, err := api.db.QueryContext(r.Context(), fmt.Sprintf(`SELECT id, at, user, action, details, ok FROM activity WHERE %s ORDER BY id DESC LIMIT 100`, strings.Join(where, " AND ")), args...)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	entries := []activityEntry{}
	for rows.Next() {
		var e activityEntry
		var details string
		if rows.Scan(&e.ID, &e.At, &e.User, &e.Action, &details, &e.OK) == nil {
			e.Details = json.RawMessage(details)
			entries = append(entries, e)
		}
	}
	users := []string{}
	if names, err := api.db.QueryContext(r.Context(), `SELECT DISTINCT user FROM activity ORDER BY user`); err == nil {
		for names.Next() {
			var name string
			if names.Scan(&name) == nil {
				users = append(users, name)
			}
		}
		names.Close()
	}
	writeJSON(w, map[string]any{"entries": entries, "users": users})
}

// pruneActivity forgets entries older than 90 days.
func (api *API) pruneActivity(ctx context.Context) {
	_, _ = api.db.ExecContext(ctx, `DELETE FROM activity WHERE at < ?`, time.Now().UTC().Add(-activityKept).Format(time.RFC3339))
}
