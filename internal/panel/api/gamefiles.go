package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
)

// A game's file manager and backups. The agent keeps every path inside the game's folder; files move
// between the browser and the agent in 2 MB pieces.

func (api *API) gameFileRoutes() {
	m := api.mux
	m.Handle("GET /api/games/{id}/files", api.gameUser(api.listGameFiles))
	m.Handle("GET /api/games/{id}/files/content", api.gameUser(api.readGameFile))
	m.Handle("PUT /api/games/{id}/files/content", api.gameUser(api.writeGameFile))
	m.Handle("POST /api/games/{id}/files/folder", api.gameUser(api.makeGameFolder))
	m.Handle("POST /api/games/{id}/files/rename", api.gameUser(api.renameGameFile))
	m.Handle("DELETE /api/games/{id}/files", api.gameUser(api.deleteGameFile))
	m.Handle("POST /api/games/{id}/files/upload", api.gameUser(api.uploadGameFile))
	m.Handle("POST /api/games/{id}/files/archive", api.gameUser(api.archiveGameFiles))
	m.Handle("POST /api/games/{id}/files/unarchive", api.gameUser(api.unarchiveGameFile))
	m.Handle("GET /api/games/{id}/files/download", api.gameUser(api.downloadGameFile))
	m.Handle("GET /api/games/{id}/backups", api.gameUser(api.listBackups))
	m.Handle("POST /api/games/{id}/backups", api.gameUser(api.makeBackupNow))
	m.Handle("PUT /api/games/{id}/backups/{backup}", api.gameUser(api.lockBackup))
	m.Handle("DELETE /api/games/{id}/backups/{backup}", api.gameUser(api.deleteBackup))
	m.Handle("POST /api/games/{id}/backups/{backup}/restore", api.gameUser(api.restoreBackup))
	m.Handle("GET /api/games/{id}/backups/{backup}/download", api.gameUser(api.downloadBackup))
}

const (
	maxUpload  = 1 << 30 // 1 GB: a world or a modpack
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

func (api *API) makeGameFolder(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	var body struct{ Path string }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "mkdir", Path: body.Path}); err != nil {
		api.agentFail(w, r, err)
		return
	}
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
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "archive", Path: body.Path, Paths: body.Paths, To: body.To}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game files archived", "user", u.Username, "game", g.ID, "to", body.To)
	w.WriteHeader(http.StatusNoContent)
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
	if _, err := api.files(r.Context(), g, protocol.GameFileArgs{Op: "unarchive", Path: body.Path}); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game file unarchived", "user", u.Username, "game", g.ID, "path", body.Path)
	w.WriteHeader(http.StatusNoContent)
}

// uploadGameFile takes a file as the request's body and sends it on to the agent in pieces.
func (api *API) uploadGameFile(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	p := r.URL.Query().Get("path")
	if over := api.overDisk(g); over != "" {
		writeError(w, http.StatusBadRequest, over)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxUpload)
	buf := make([]byte, filePiece)
	var offset int64
	for {
		n, err := io.ReadFull(body, buf)
		if n > 0 || offset == 0 {
			if _, ferr := api.files(r.Context(), g, protocol.GameFileArgs{Op: "append", Path: p, Data: buf[:n], Offset: offset}); ferr != nil {
				api.agentFail(w, r, ferr)
				return
			}
			offset += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "the upload didn't finish (files can be up to 1 GB)")
			return
		}
	}
	api.log.Info("game file uploaded", "user", u.Username, "game", g.ID, "path", p, "bytes", offset)
	w.WriteHeader(http.StatusNoContent)
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
	file      string
}

func (api *API) listBackups(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT id, name, size, locked, state, created_at FROM game_backups WHERE game_id = ? ORDER BY id DESC`, g.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []backupView{}
	for rows.Next() {
		var b backupView
		if rows.Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.CreatedAt) == nil {
			list = append(list, b)
		}
	}
	writeJSON(w, list)
}

func (api *API) backup(ctx context.Context, g *game, id int64) (*backupView, error) {
	var b backupView
	err := api.db.QueryRowContext(ctx, `SELECT id, name, size, locked, state, created_at, file FROM game_backups WHERE id = ? AND game_id = ?`, id, g.ID).
		Scan(&b.ID, &b.Name, &b.Size, &b.Locked, &b.State, &b.CreatedAt, &b.file)
	if err != nil {
		return nil, errors.New("no such backup")
	}
	return &b, nil
}

// startBackup makes a backup in the background: a big world takes a while.
func (api *API) startBackup(ctx context.Context, g *game, name string) (int64, error) {
	if name == "" {
		name = "Backup " + time.Now().Format("2 Jan 2006 15:04")
	}
	file := fmt.Sprintf("b-%d.tar.gz", time.Now().UnixNano())
	res, err := api.db.ExecContext(ctx, `INSERT INTO game_backups (game_id, file, name) VALUES (?, ?, ?)`, g.ID, file, clipText(name, 80))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		var result protocol.GameBackupResult
		err := api.hub.Request(ctx, g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "make", Name: file}, &result)
		state := "done"
		if err != nil {
			state = "failed"
			api.log.Warn("backup failed", "game", g.ID, "err", err)
		}
		_, _ = api.db.Exec(`UPDATE game_backups SET state = ?, size = ? WHERE id = ?`, state, result.Size, id)
	}()
	return id, nil
}

func (api *API) makeBackupNow(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Name string }
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	id, err := api.startBackup(r.Context(), g, strings.TrimSpace(body.Name))
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("backup started", "user", u.Username, "game", g.ID)
	b, _ := api.backup(r.Context(), g, id)
	writeJSON(w, b)
}

func (api *API) lockBackup(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	id, _ := strconv.ParseInt(r.PathValue("backup"), 10, 64)
	var body struct{ Locked bool }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE game_backups SET locked = ? WHERE id = ? AND game_id = ?`, body.Locked, id, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
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
		if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "delete", Name: b.file}, nil); err != nil {
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
	if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "restore", Name: b.file}, nil); err != nil {
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
	name := strings.NewReplacer(" ", "-", ":", "").Replace(g.Name+"-"+b.CreatedAt[:10]) + ".tar.gz"
	err = sendPieces(w, name, func(offset int64) ([]byte, int64, error) {
		var res protocol.GameBackupResult
		err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameBackup, protocol.GameBackupArgs{ID: g.ID, Op: "download", Name: b.file, Offset: offset}, &res)
		return res.Data, res.Size, err
	})
	if err != nil {
		api.agentFail(w, r, err)
	}
}
