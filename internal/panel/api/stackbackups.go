package api

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/cron"
	"github.com/Atrello/bastion/internal/protocol"
)

// Backups of compose stacks: the stack's folder and the volumes it made, made by its server's agent
// (internal/agent/stackbackups.go) and kept there or in backup storage. Each stack has its settings: a
// schedule, how many to keep, whether it's stopped while backed up (so a database is copied whole), and
// where they go. Only the owner deals with stacks.

type stackBackupView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Locked    bool   `json:"locked"`
	State     string `json:"state"`
	Error     string `json:"error"`
	Store     string `json:"store"`
	CreatedAt string `json:"createdAt"`
	file      string
	storeID   sql.NullInt64
}

type stackBackupSettings struct {
	When      string `json:"when"` // cron, or "" for no schedule
	Keep      int    `json:"keep"`
	Stop      bool   `json:"stop"`
	StoreID   int64  `json:"storeId"`
	LastRunAt string `json:"lastRunAt"`
	LastError string `json:"lastError"`
	NextRun   string `json:"nextRun"`
}

func (api *API) stackBackupRoutes() {
	m := api.mux
	const base = "/api/servers/{id}/stacks/{name}/backups"
	m.Handle("GET "+base, api.owner(api.listStackBackups))
	m.Handle("POST "+base, api.owner(api.makeStackBackupNow))
	m.Handle("PUT "+base+"/settings", api.owner(api.saveStackBackupSettings))
	m.Handle("PUT "+base+"/{backup}", api.owner(api.lockStackBackup))
	m.Handle("DELETE "+base+"/{backup}", api.owner(api.deleteStackBackup))
	m.Handle("POST "+base+"/{backup}/restore", api.owner(api.restoreStackBackup))
	m.Handle("GET "+base+"/{backup}/download", api.owner(api.downloadStackBackup))
}

func (api *API) stackSettings(ctx context.Context, server int64, stack string) stackBackupSettings {
	s := stackBackupSettings{Keep: 7, Stop: true}
	var last sql.NullString
	var store sql.NullInt64
	_ = api.db.QueryRowContext(ctx, `SELECT cron, keep, stop, store_id, last_run_at, last_error FROM stack_backup_settings WHERE server_id = ? AND stack = ?`, server, stack).
		Scan(&s.When, &s.Keep, &s.Stop, &store, &last, &s.LastError)
	s.StoreID, s.LastRunAt = store.Int64, last.String
	if c, err := cron.Parse(s.When); s.When != "" && err == nil {
		if next := c.Next(time.Now()); !next.IsZero() {
			s.NextRun = next.UTC().Format(time.RFC3339)
		}
	}
	return s
}

func (api *API) listStackBackups(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	rows, err := api.db.QueryContext(r.Context(), `SELECT b.id, b.name, b.size, b.locked, b.state, b.error, coalesce(s.name, ''), b.created_at
		FROM stack_backups b LEFT JOIN backup_stores s ON s.id = b.store_id WHERE b.server_id = ? AND b.stack = ? ORDER BY b.id DESC`, id, name)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []stackBackupView{}
	for rows.Next() {
		var b stackBackupView
		if rows.Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.Error, &b.Store, &b.CreatedAt) == nil {
			list = append(list, b)
		}
	}
	rows.Close()
	writeJSON(w, map[string]any{"backups": list, "settings": api.stackSettings(r.Context(), id, name)})
}

func (api *API) stackBackup(ctx context.Context, server int64, stack string, id int64) (*stackBackupView, error) {
	var b stackBackupView
	err := api.db.QueryRowContext(ctx, `SELECT b.id, b.name, b.size, b.locked, b.state, b.error, coalesce(s.name, ''), b.created_at, b.file, b.store_id
		FROM stack_backups b LEFT JOIN backup_stores s ON s.id = b.store_id WHERE b.id = ? AND b.server_id = ? AND b.stack = ?`, id, server, stack).
		Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.Error, &b.Store, &b.CreatedAt, &b.file, &b.storeID)
	if err != nil {
		return nil, fmt.Errorf("no such backup")
	}
	return &b, nil
}

func stackKey(server int64, stack, file string) string {
	return "stack-" + strconv.FormatInt(server, 10) + "-" + stack + "/" + file
}

// startStackBackup makes a backup in the background, named like game backups ("Nightly-2026-11-09-0400"),
// then keeps only the newest the stack's settings say.
func (api *API) startStackBackup(ctx context.Context, server int64, stack, name string) (int64, error) {
	settings := api.stackSettings(ctx, server, stack)
	if name == "" {
		name = "Backup"
	}
	name = clipText(name, 60) + "-" + time.Now().Format("2006-01-02-1504")
	file := fmt.Sprintf("%s-%04x.tar.gz", fileName(name, 50), rand.IntN(1<<16))
	store := sql.NullInt64{Int64: settings.StoreID, Valid: settings.StoreID != 0}
	target, err := api.s3TargetAt(ctx, store, stackKey(server, stack, file))
	if err != nil {
		return 0, err
	}
	res, err := api.db.ExecContext(ctx, `INSERT INTO stack_backups (server_id, stack, file, name, store_id) VALUES (?, ?, ?, ?, ?)`, server, stack, file, name, store)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var result protocol.GameBackupResult
		err := api.hub.RequestWith(ctx, server, protocol.OpStackBackup,
			protocol.StackBackupArgs{Stack: stack, Op: "make", Name: file, Stop: settings.Stop, S3: target}, &result, agents.RequestOptions{Limit: 6 * time.Hour})
		state, message := "done", ""
		if err != nil {
			state = "failed"
			_, message = api.agentProblem("stack backup", err)
			api.log.Warn("stack backup failed", "server", server, "stack", stack, "err", err)
		}
		_, _ = api.db.Exec(`UPDATE stack_backups SET state = ?, size = ?, error = ? WHERE id = ?`, state, result.Size, message, id)
		if err == nil && settings.Keep > 0 {
			api.pruneStackBackups(context.Background(), server, stack, settings.Keep)
		}
	}()
	return id, nil
}

// pruneStackBackups deletes a stack's oldest backups past the newest keep, leaving locked ones alone.
func (api *API) pruneStackBackups(ctx context.Context, server int64, stack string, keep int) {
	rows, err := api.db.QueryContext(ctx, `SELECT id FROM stack_backups WHERE server_id = ? AND stack = ? AND state = 'done' AND locked = 0 ORDER BY id DESC LIMIT -1 OFFSET ?`, server, stack, keep)
	if err != nil {
		return
	}
	var old []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			old = append(old, id)
		}
	}
	rows.Close()
	for _, id := range old {
		if err := api.removeStackBackup(ctx, server, stack, id); err != nil {
			api.log.Warn("an old stack backup couldn't be deleted", "server", server, "stack", stack, "err", err)
		}
	}
}

func (api *API) removeStackBackup(ctx context.Context, server int64, stack string, id int64) error {
	b, err := api.stackBackup(ctx, server, stack, id)
	if err != nil {
		return err
	}
	if b.State == "done" {
		target, err := api.s3TargetAt(ctx, b.storeID, stackKey(server, stack, b.file))
		if err != nil {
			return err
		}
		if err := api.hub.Request(ctx, server, protocol.OpStackBackup, protocol.StackBackupArgs{Stack: stack, Op: "delete", Name: b.file, S3: target}, nil); err != nil {
			return err
		}
	}
	_, err = api.db.ExecContext(ctx, `DELETE FROM stack_backups WHERE id = ?`, id)
	return err
}

func (api *API) makeStackBackupNow(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var body struct{ Name string }
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	if !api.hub.Live(id).Online {
		writeError(w, http.StatusServiceUnavailable, "that server's agent isn't connected")
		return
	}
	backup, err := api.startStackBackup(r.Context(), id, name, strings.TrimSpace(body.Name))
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("stack backup started", "user", u.Username, "server", id, "stack", name)
	b, _ := api.stackBackup(r.Context(), id, name, backup)
	writeJSON(w, b)
}

func (api *API) saveStackBackupSettings(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var body stackBackupSettings
	if !readJSON(w, r, &body) {
		return
	}
	body.When = strings.TrimSpace(body.When)
	if body.When != "" {
		if _, err := cron.Parse(body.When); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Keep < 0 || body.Keep > 1000 {
		writeError(w, http.StatusBadRequest, "keep from 0 (all) to 1,000 backups")
		return
	}
	store := sql.NullInt64{Int64: body.StoreID, Valid: body.StoreID != 0}
	if store.Valid {
		if _, err := api.store(r.Context(), body.StoreID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	_, err := api.db.ExecContext(r.Context(), `INSERT INTO stack_backup_settings (server_id, stack, cron, keep, stop, store_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (server_id, stack) DO UPDATE SET cron = excluded.cron, keep = excluded.keep, stop = excluded.stop, store_id = excluded.store_id`,
		id, name, body.When, body.Keep, body.Stop, store)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("stack backup settings saved", "user", u.Username, "server", id, "stack", name, "when", body.When, "keep", strconv.Itoa(body.Keep))
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) lockStackBackup(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var body struct{ Locked bool }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE stack_backups SET locked = ? WHERE id = ? AND server_id = ? AND stack = ?`, body.Locked, r.PathValue("backup"), id, name); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info(map[bool]string{true: "stack backup locked", false: "stack backup unlocked"}[body.Locked], "user", u.Username, "server", id, "stack", name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteStackBackup(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	backup, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.stackBackup(r.Context(), id, name, backup)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if b.Locked {
		writeError(w, http.StatusBadRequest, "it's locked: unlock it first")
		return
	}
	if err := api.removeStackBackup(r.Context(), id, name, backup); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("stack backup deleted", "user", u.Username, "server", id, "stack", name, "backup", b.Name)
	w.WriteHeader(http.StatusNoContent)
}

// restoreStackBackup puts a backup back, as a job: the stack's folder and volumes are emptied and filled
// from the backup, with the stack stopped meanwhile and started again after if it was running.
func (api *API) restoreStackBackup(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	backup, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.stackBackup(r.Context(), id, name, backup)
	if err != nil || b.State != "done" {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	}
	target, err := api.s3TargetAt(r.Context(), b.storeID, stackKey(id, name, b.file))
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("stack backup restored", "user", u.Username, "server", id, "stack", name, "backup", b.Name)
	api.startJob(w, u, 0, "Restoring "+name, b.Name, "Stopped part-way: what was restored so far stays.", func(ctx context.Context, opts agents.RequestOptions) error {
		return api.hub.RequestWith(ctx, id, protocol.OpStackBackup, protocol.StackBackupArgs{Stack: name, Op: "restore", Name: b.file, Stop: true, S3: target}, nil, opts)
	})
}

func (api *API) downloadStackBackup(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	backup, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.stackBackup(r.Context(), id, name, backup)
	if err != nil || b.State != "done" {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	}
	target, err := api.s3TargetAt(r.Context(), b.storeID, stackKey(id, name, b.file))
	if err != nil {
		api.fail(w, r, err)
		return
	}
	err = sendPieces(w, fileName(name+"-"+b.Name, 120)+".tar.gz", func(offset int64) ([]byte, int64, error) {
		var res protocol.GameBackupResult
		err := api.hub.Request(r.Context(), id, protocol.OpStackBackup, protocol.StackBackupArgs{Stack: name, Op: "download", Name: b.file, Offset: offset, S3: target}, &res)
		return res.Data, res.Size, err
	})
	if err != nil {
		api.agentFail(w, r, err)
	}
}

// runStackBackupSchedules starts the stack backups due this minute (from runSchedules).
func (api *API) runStackBackupSchedules(ctx context.Context, minute time.Time) {
	rows, err := api.db.QueryContext(ctx, `SELECT server_id, stack, cron FROM stack_backup_settings WHERE cron != ''`)
	if err != nil {
		return
	}
	type due struct {
		server int64
		stack  string
	}
	var list []due
	for rows.Next() {
		var d due
		var when string
		if rows.Scan(&d.server, &d.stack, &when) != nil {
			continue
		}
		if c, err := cron.Parse(when); err == nil && c.Matches(minute) {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		message := ""
		if !api.hub.Live(d.server).Online {
			message = "its server's agent wasn't connected"
		} else if _, err := api.startStackBackup(ctx, d.server, d.stack, "Scheduled"); err != nil {
			message = err.Error()
		}
		_, _ = api.db.ExecContext(ctx, `UPDATE stack_backup_settings SET last_run_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), last_error = ? WHERE server_id = ? AND stack = ?`, message, d.server, d.stack)
	}
}
