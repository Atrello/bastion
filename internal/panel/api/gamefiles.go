package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
)

// A game's file manager and backups. The agent keeps every path inside the game's folder; files move
// between the browser and the agent in 2 MB pieces.

func (api *API) gameFileRoutes() {
	m := api.mux
	m.Handle("GET /api/games/{id}/files", api.gameCan("files", api.listGameFiles))
	m.Handle("GET /api/games/{id}/files/content", api.gameCan("files", api.readGameFile))
	m.Handle("PUT /api/games/{id}/files/content", api.gameCan("files", api.writeGameFile))
	m.Handle("POST /api/games/{id}/files/folder", api.gameCan("files", api.makeGameFolder))
	m.Handle("POST /api/games/{id}/files/rename", api.gameCan("files", api.renameGameFile))
	m.Handle("DELETE /api/games/{id}/files", api.gameCan("files", api.deleteGameFile))
	m.Handle("POST /api/games/{id}/files/upload", api.gameCan("files", api.uploadGameFile))
	m.Handle("POST /api/games/{id}/files/archive", api.gameCan("files", api.archiveGameFiles))
	m.Handle("POST /api/games/{id}/files/unarchive", api.gameCan("files", api.unarchiveGameFile))
	m.Handle("POST /api/games/{id}/files/move", api.gameCan("files", api.moveGameFiles))
	m.Handle("GET /api/games/{id}/files/download", api.gameCan("files", api.downloadGameFile))
	m.Handle("GET /api/games/{id}/backups", api.gameCan("backups", api.listBackups))
	m.Handle("POST /api/games/{id}/backups", api.gameCan("backups", api.makeBackupNow))
	m.Handle("PUT /api/games/{id}/backups/{backup}", api.gameCan("backups", api.lockBackup))
	m.Handle("DELETE /api/games/{id}/backups/{backup}", api.gameCan("backups", api.deleteBackup))
	m.Handle("POST /api/games/{id}/backups/{backup}/restore", api.gameCan("backups", api.restoreBackup))
	m.Handle("GET /api/games/{id}/backups/{backup}/download", api.gameCan("backups", api.downloadBackup))
}

const (
	maxUpload  = 50 << 30 // 50 GB, as much as an archive may unpack to; a game's disk limit can make it less
	filePiece  = 2 << 20
	maxEditing = 2 << 20
)

func (api *API) files(ctx context.Context, g *game, args protocol.GameFileArgs) (*protocol.GameFileResult, error) {
	args.ID = g.ID
	var res protocol.GameFileResult
	err := api.hub.Request(ctx, g.ServerID, protocol.OpGameFiles, args, &res)
	return &res, err
}

func (api *API) listGameFiles(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	res, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "list", Path: r.URL.Query().Get("path")})
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	files := res.Files
	if files == nil {
		files = []protocol.GameFile{}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Dir != files[j].Dir {
			return files[i].Dir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	writeJSON(w, files)
}

func (api *API) readGameFile(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	res, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "read", Path: r.URL.Query().Get("path")})
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"content": string(res.Data)})
}

func (api *API) writeGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Path, Content string }
	if !readJSONLimit(w, r, &body, maxEditing+(64<<10)) {
		return
	}
	if len(body.Content) > maxEditing {
		writeError(w, http.StatusBadRequest, "that's too big to save from the editor: upload it instead")
		return
	}
	if over := api.overDisk(g); over != "" {
		writeError(w, http.StatusBadRequest, over)
		return
	}
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "write", Path: body.Path, Data: []byte(body.Content)}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game file saved", "user", u.Username, "game", g.ID, "path", body.Path)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) makeGameFolder(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Path string }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "mkdir", Path: body.Path}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game folder made", "user", u.Username, "game", g.ID, "path", body.Path)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) renameGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ From, To string }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "rename", Path: body.From, To: body.To}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game file renamed", "user", u.Username, "game", g.ID, "from", body.From, "to", body.To)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	p := r.URL.Query().Get("path")
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "delete", Path: p}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game file deleted", "user", u.Username, "game", g.ID, "path", p)
	w.WriteHeader(http.StatusNoContent)
}

// archiveGameFiles packs files and folders from one folder into a .tar.gz there.
func (api *API) archiveGameFiles(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
		To    string   `json:"to"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if over := api.overDisk(g); over != "" {
		writeError(w, http.StatusBadRequest, over)
		return
	}
	api.gameFilesJob(w, u, g, "Archiving into "+path.Base(body.To), g.Name+", "+folderOf(body.Path),
		protocol.GameFileArgs{Op: "archive", Path: body.Path, Paths: body.Paths, To: body.To})
}

// moveGameFiles moves files and folders from one folder into another.
func (api *API) moveGameFiles(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
		To    string   `json:"to"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	what := fmt.Sprintf("%d items", len(body.Paths))
	if len(body.Paths) == 1 {
		what = body.Paths[0]
	}
	api.gameFilesJob(w, u, g, "Moving "+what, g.Name+", to "+folderOf(body.To),
		protocol.GameFileArgs{Op: "move", Path: body.Path, Paths: body.Paths, To: body.To})
}

// unarchiveGameFile unpacks a .zip, .tar.gz or .tar into the folder it's in.
func (api *API) unarchiveGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Path string }
	if !readJSON(w, r, &body) {
		return
	}
	if over := api.overDisk(g); over != "" {
		writeError(w, http.StatusBadRequest, over)
		return
	}
	api.gameFilesJob(w, u, g, "Unpacking "+path.Base(body.Path), g.Name+", "+folderOf(path.Dir("/"+body.Path)),
		protocol.GameFileArgs{Op: "unarchive", Path: body.Path})
}

// folderOf is a folder as it's shown: "/plugins/", or "/" for the game's own.
func folderOf(p string) string {
	p = path.Clean("/" + p)
	if p == "/" {
		return p
	}
	return p + "/"
}

// uploadGameFile takes a file as the request's body and sends it on to the agent in pieces.
func (api *API) uploadGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	p := r.URL.Query().Get("path")
	if over := api.overDisk(g); over != "" {
		writeError(w, http.StatusBadRequest, over)
		return
	}
	// It arrives in pieces, so its size doesn't matter to the panel's memory, only to the disk: an upload
	// can't take a game past its disk limit, if it has one.
	limit := int64(maxUpload)
	if g.DiskMB > 0 {
		v := api.view(g, "", false)
		limit = min(limit, int64(v.DiskLimit)-int64(v.DiskUsed))
	}
	if r.ContentLength > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "that's bigger than "+tooBig(g, limit))
		return
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	buf := make([]byte, filePiece)
	var offset int64
	// fill reads a piece. Only io.EOF means the whole file has come: a body cut short (the browser closed,
	// the upload cancelled) reads as io.ErrUnexpectedEOF, which io.ReadFull would make look the same.
	fill := func() (n int, err error) {
		for n < len(buf) && err == nil {
			var m int
			m, err = body.Read(buf[n:])
			n += m
		}
		return n, err
	}
	removePart := func() {
		_, _ = api.files(context.WithoutCancel(r.Context()), g, protocol.GameFileArgs{Op: "delete", Path: p})
	}
	for {
		n, err := fill()
		if n > 0 || offset == 0 {
			if _, ferr := api.files(r.Context(), g, protocol.GameFileArgs{Op: "append", Path: p, Data: buf[:n], Offset: offset}); ferr != nil {
				// The browser went (which cancels the request) or the agent did: either way it's not all there.
				removePart()
				api.agentFail(w, r, ferr)
				return
			}
			offset += int64(n)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) && (r.ContentLength < 0 || offset == r.ContentLength) {
			break
		}
		// Not all of it came: what did is removed rather than left looking like the whole file.
		removePart()
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "it's bigger than "+tooBig(g, limit))
		} else {
			writeError(w, http.StatusBadRequest, "the upload didn't finish, so the part that came was removed")
		}
		api.log.Info("game file upload cut short", "user", u.Username, "game", g.ID, "path", p, "bytes", offset)
		return
	}
	api.log.Info("game file uploaded", "user", u.Username, "game", g.ID, "path", p, "bytes", offset)
	w.WriteHeader(http.StatusNoContent)
}

// tooBig says what an upload can be at most, for when one is bigger.
func tooBig(g *game, limit int64) string {
	if limit < maxUpload {
		return fmt.Sprintf("the room left under its %d MB disk limit (%d MB)", g.DiskMB, max(limit, 0)>>20)
	}
	return "50 GB, the most one upload can be"
}

// fileName is s in letters any file system takes: lower case letters, digits and dashes, at most max.
func fileName(s string, max int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > max {
		out = strings.TrimRight(out[:max], "-")
	}
	if out == "" {
		return "backup"
	}
	return out
}

// pruneGameBackups deletes a game's oldest backups past the newest keep, leaving locked ones alone.
func (api *API) pruneGameBackups(ctx context.Context, g *game, keep int) {
	rows, err := api.db.QueryContext(ctx, `SELECT id FROM game_backups WHERE game_id = ? AND state = 'done' AND locked = 0 ORDER BY id DESC LIMIT -1 OFFSET ?`, g.ID, keep)
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
		b, err := api.backup(ctx, g, id)
		if err != nil {
			continue
		}
		target, err := api.s3Target(ctx, b.storeID, g.ID, b.file)
		if err != nil {
			continue
		}
		if err := api.hub.Request(ctx, g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "delete", Name: b.file, S3: target}, nil); err != nil {
			api.log.Warn("an old backup couldn't be deleted", "game", g.ID, "backup", b.Name, "err", err)
			continue
		}
		_, _ = api.db.ExecContext(ctx, `DELETE FROM game_backups WHERE id = ?`, id)
	}
}

// sizeText is a size as people read it: "4.2 GB".
func sizeText(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// sendPieces streams a file from the agent to the browser as a download, a piece at a time.
func sendPieces(w http.ResponseWriter, name string, piece func(offset int64) ([]byte, int64, error)) error {
	data, size, err := piece(0)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	for offset := int64(0); ; {
		if _, err := w.Write(data); err != nil {
			return nil // the browser went away
		}
		offset += int64(len(data))
		if offset >= size || len(data) == 0 {
			return nil
		}
		if data, _, err = piece(offset); err != nil {
			return nil // too late to say: the download is cut short
		}
	}
}

func (api *API) downloadGameFile(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	p := r.URL.Query().Get("path")
	err := sendPieces(w, path.Base("/"+p), func(offset int64) ([]byte, int64, error) {
		res, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "download", Path: p, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		return res.Data, res.Size, nil
	})
	if err != nil {
		api.agentFail(w, r, err)
	}
}

// Backups.

type backupView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Locked    bool   `json:"locked"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	Store     string `json:"store"` // the backup storage it's in, "" for the game's own server
	Links     int    `json:"links"` // download links to it still working (sent in notifications)
	file      string
	storeID   sql.NullInt64
}

func (api *API) listBackups(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT b.id, b.name, b.size, b.locked, b.state, b.created_at, coalesce(s.name, ''),
		(SELECT COUNT(*) FROM backup_links l WHERE l.backup_id = b.id AND l.expires_at > ?)
		FROM game_backups b LEFT JOIN backup_stores s ON s.id = b.store_id WHERE b.game_id = ? ORDER BY b.id DESC`, time.Now().UTC().Format(time.RFC3339), g.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []backupView{}
	for rows.Next() {
		var b backupView
		if rows.Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.CreatedAt, &b.Store, &b.Links) == nil {
			list = append(list, b)
		}
	}
	writeJSON(w, list)
}

func (api *API) backup(ctx context.Context, g *game, id int64) (*backupView, error) {
	var b backupView
	err := api.db.QueryRowContext(ctx, `SELECT b.id, b.name, b.size, b.locked, b.state, b.created_at, b.file, b.store_id, coalesce(s.name, '')
		FROM game_backups b LEFT JOIN backup_stores s ON s.id = b.store_id WHERE b.id = ? AND b.game_id = ?`, id, g.ID).
		Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.CreatedAt, &b.file, &b.storeID, &b.Store)
	if err != nil {
		return nil, errors.New("no such backup")
	}
	return &b, nil
}

// startBackup makes a backup in the background: a big world takes a while.
// Its name is the one given (or "Backup") with the date and time: "Nightly-2026-11-09-1915". Its file, on
// the server or in backup storage, is called the same, in letters any file system takes, with a few
// random ones so two made in the same minute don't clash.
func (api *API) startBackup(ctx context.Context, g *game, name string, keep int) (int64, error) {
	if name == "" {
		name = "Backup"
	}
	stamp := time.Now().Format("2006-01-02-1504")
	name = clipText(name, 60) + "-" + stamp
	file := fmt.Sprintf("%s-%04x.tar.gz", fileName(name, 50), rand.IntN(1<<16)) // the name has the date already
	store := sql.NullInt64{Int64: g.BackupStore, Valid: g.BackupStore != 0}
	target, err := api.s3Target(ctx, store, g.ID, file)
	if err != nil {
		return 0, err
	}
	res, err := api.db.ExecContext(ctx, `INSERT INTO game_backups (game_id, file, name, store_id) VALUES (?, ?, ?, ?)`, g.ID, file, name, store)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var result protocol.GameBackupResult
		err := api.hub.RequestWith(ctx, g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "make", Name: file, S3: target}, &result, agents.RequestOptions{Limit: 6 * time.Hour})
		state := "done"
		if err != nil {
			state = "failed"
			api.log.Warn("backup failed", "game", g.ID, "err", err)
		}
		_, _ = api.db.Exec(`UPDATE game_backups SET state = ?, size = ? WHERE id = ?`, state, result.Size, id)
		values := map[string]string{"backup": name, "size": sizeText(result.Size)}
		if err != nil {
			_, values["error"] = api.agentProblem("backup", err)
			api.notify("backup_failed", g, id, values)
		} else {
			api.notify("backup_done", g, id, values)
			if keep > 0 {
				api.pruneGameBackups(context.Background(), g, keep)
			}
		}
	}()
	return id, nil
}

func (api *API) makeBackupNow(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Name string }
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	id, err := api.startBackup(r.Context(), g, strings.TrimSpace(body.Name), 0)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("backup started", "user", u.Username, "game", g.ID)
	b, _ := api.backup(r.Context(), g, id)
	writeJSON(w, b)
}

func (api *API) lockBackup(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	var body struct{ Locked bool }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE game_backups SET locked = ? WHERE id = ? AND game_id = ?`, body.Locked, id, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info(map[bool]string{true: "backup locked", false: "backup unlocked"}[body.Locked], "user", u.Username, "game", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteBackup(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.backup(r.Context(), g, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if b.Locked {
		writeError(w, http.StatusBadRequest, "it's locked: unlock it first")
		return
	}
	if b.State != "failed" {
		target, err := api.s3Target(r.Context(), b.storeID, g.ID, b.file)
		if err != nil {
			api.fail(w, r, err)
			return
		}
		if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "delete", Name: b.file, S3: target}, nil); err != nil {
			api.agentFail(w, r, err)
			return
		}
	}
	_, _ = api.db.ExecContext(r.Context(), `DELETE FROM game_backups WHERE id = ?`, id)
	api.log.Info("backup deleted", "user", u.Username, "game", g.ID, "backup", b.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) restoreBackup(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.backup(r.Context(), g, id)
	if err != nil || b.State != "done" {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	}
	target, err := api.s3Target(r.Context(), b.storeID, g.ID, b.file)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if err := api.hub.RequestWith(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "restore", Name: b.file, S3: target}, nil, agents.RequestOptions{Limit: 6 * time.Hour}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("backup restored", "user", u.Username, "game", g.ID, "backup", b.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) downloadBackup(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	b, err := api.backup(r.Context(), g, id)
	if err != nil || b.State != "done" {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	}
	api.sendBackup(w, r, g, b)
}

// sendBackup streams a backup to the browser, from the game's server or its backup storage.
func (api *API) sendBackup(w http.ResponseWriter, r *http.Request, g *game, b *backupView) {
	name := fileName(g.Name+"-"+b.Name, 120) + ".tar.gz" // survival-smp-nightly-2026-11-09-1915.tar.gz
	target, err := api.s3Target(r.Context(), b.storeID, g.ID, b.file)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	err = sendPieces(w, name, func(offset int64) ([]byte, int64, error) {
		var res protocol.GameBackupResult
		err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "download", Name: b.file, Offset: offset, S3: target}, &res)
		return res.Data, res.Size, err
	})
	if err != nil {
		api.agentFail(w, r, err)
	}
}
