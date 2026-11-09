package api

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
)

// Backup storage: S3 buckets (or services that speak S3) where games can keep their backups instead of on
// their own server. Only the owner manages them. Each is checked when saved, by writing and removing a
// small file. The secret key is never sent back: the form leaves it empty to keep it.

type backupStore struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey,omitempty"` // in, never out
	PathStyle bool   `json:"pathStyle"`
	PlainHTTP bool   `json:"plainHttp"`
	Backups   int    `json:"backups"` // how many backups are in it
}

func (api *API) backupStoreRoutes() {
	m := api.mux
	m.Handle("GET /api/backup-stores", api.owner(api.listBackupStores))
	m.Handle("POST /api/backup-stores", api.owner(api.saveBackupStore))
	m.Handle("PUT /api/backup-stores/{id}", api.owner(api.saveBackupStore))
	m.Handle("DELETE /api/backup-stores/{id}", api.owner(api.deleteBackupStore))
}

const storeColumns = `id, name, endpoint, region, bucket, prefix, access_key, secret_key, path_style, plain_http`

func scanStore(row interface{ Scan(...any) error }) (*backupStore, error) {
	var s backupStore
	err := row.Scan(&s.ID, &s.Name, &s.Endpoint, &s.Region, &s.Bucket, &s.Prefix, &s.AccessKey, &s.SecretKey, &s.PathStyle, &s.PlainHTTP)
	return &s, err
}

func (api *API) store(ctx context.Context, id int64) (*backupStore, error) {
	s, err := scanStore(api.db.QueryRowContext(ctx, `SELECT `+storeColumns+` FROM backup_stores WHERE id = ?`, id))
	if err != nil {
		return nil, errors.New("there's no such backup storage")
	}
	return s, nil
}

func (api *API) listBackupStores(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT `+storeColumns+`, (SELECT COUNT(*) FROM game_backups WHERE store_id = backup_stores.id) FROM backup_stores ORDER BY name`)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []backupStore{}
	for rows.Next() {
		var s backupStore
		if rows.Scan(&s.ID, &s.Name, &s.Endpoint, &s.Region, &s.Bucket, &s.Prefix, &s.AccessKey, &s.SecretKey, &s.PathStyle, &s.PlainHTTP, &s.Backups) == nil {
			s.SecretKey = ""
			list = append(list, s)
		}
	}
	writeJSON(w, list)
}

// check tidies what was typed in, and says what's wrong with it.
func (s *backupStore) check() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	if rest, ok := strings.CutPrefix(s.Endpoint, "https://"); ok {
		s.Endpoint = rest
	} else if rest, ok := strings.CutPrefix(s.Endpoint, "http://"); ok {
		s.Endpoint, s.PlainHTTP = rest, true
	}
	s.Endpoint = strings.TrimSuffix(s.Endpoint, "/")
	s.Bucket, s.Region, s.AccessKey = strings.TrimSpace(s.Bucket), strings.TrimSpace(s.Region), strings.TrimSpace(s.AccessKey)
	s.Prefix = strings.Trim(strings.TrimSpace(s.Prefix), "/")
	if s.Prefix != "" {
		s.Prefix += "/"
	}
	host := s.Endpoint
	if h, _, err := net.SplitHostPort(s.Endpoint); err == nil {
		host = h
	}
	switch {
	case s.Name == "" || len(s.Name) > 60:
		return errors.New("give it a name, up to 60 letters")
	case host == "" || strings.ContainsAny(s.Endpoint, "/ ?#@"):
		return errors.New("the address is the storage's host name, like s3.eu-west-2.amazonaws.com")
	case len(s.Bucket) < 3 || len(s.Bucket) > 63 || strings.ContainsAny(s.Bucket, "/ "):
		return errors.New("that isn't a bucket name")
	case strings.Contains(s.Prefix, "..") || len(s.Prefix) > 200:
		return errors.New("that folder can't be used")
	case s.AccessKey == "" || s.SecretKey == "":
		return errors.New("it needs an access key and its secret")
	case s.PlainHTTP && !privateHost(host):
		// Keys and backups would cross the internet unencrypted.
		return errors.New("plain http is only for storage on your own network: use https")
	}
	return nil
}

func privateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

func (s *backupStore) client() (*minio.Client, error) {
	lookup := minio.BucketLookupDNS
	if s.PathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(s.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(s.AccessKey, s.SecretKey, ""), Secure: !s.PlainHTTP, Region: s.Region, BucketLookup: lookup,
	})
}

// test writes a small file in the store and removes it, so a wrong key or bucket shows now, not at the
// first backup.
func (s *backupStore) test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := s.client()
	if err != nil {
		return err
	}
	key := s.Prefix + ".bastion-check"
	check := []byte("Bastion checking it can write here.")
	if _, err := c.PutObject(ctx, s.Bucket, key, bytes.NewReader(check), int64(len(check)), minio.PutObjectOptions{DisableContentSha256: true, SendContentMd5: true}); err != nil {
		resp := minio.ToErrorResponse(err)
		switch resp.Code {
		case "NoSuchBucket":
			return errors.New("there's no such bucket there")
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			return fmt.Errorf("it refused the keys (%s)", resp.Code)
		}
		return fmt.Errorf("it couldn't be written to: %v", err)
	}
	if err := c.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("a file could be written but not deleted (backups couldn't be deleted either): %v", err)
	}
	return nil
}

func (api *API) saveBackupStore(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var s backupStore
	if !readJSON(w, r, &s) {
		return
	}
	var old *backupStore
	if r.Method == http.MethodPut {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var err error
		old, err = api.store(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.ID = id
		if s.SecretKey == "" {
			s.SecretKey = old.SecretKey // left empty: keep it
		}
	}
	if err := s.check(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if old != nil && (s.Endpoint != old.Endpoint || s.Bucket != old.Bucket || s.Prefix != old.Prefix) {
		var n int
		_ = api.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM game_backups WHERE store_id = ?`, s.ID).Scan(&n)
		if n > 0 {
			// The backups in it are found by where it is: moved, they'd be lost track of.
			writeError(w, http.StatusBadRequest, "it has backups in it, so its address, bucket and folder can't change: add new storage for somewhere else")
			return
		}
	}
	if err := s.test(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, "Bastion couldn't use that storage: "+err.Error())
		return
	}
	var err error
	if s.ID == 0 {
		_, err = api.db.ExecContext(r.Context(), `INSERT INTO backup_stores (name, endpoint, region, bucket, prefix, access_key, secret_key, path_style, plain_http) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.Name, s.Endpoint, s.Region, s.Bucket, s.Prefix, s.AccessKey, s.SecretKey, s.PathStyle, s.PlainHTTP)
	} else {
		_, err = api.db.ExecContext(r.Context(), `UPDATE backup_stores SET name = ?, endpoint = ?, region = ?, bucket = ?, prefix = ?, access_key = ?, secret_key = ?, path_style = ?, plain_http = ? WHERE id = ?`,
			s.Name, s.Endpoint, s.Region, s.Bucket, s.Prefix, s.AccessKey, s.SecretKey, s.PathStyle, s.PlainHTTP, s.ID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusBadRequest, "there's already backup storage called that")
			return
		}
		api.fail(w, r, err)
		return
	}
	api.log.Info("backup storage saved", "user", u.Username, "name", s.Name, "endpoint", s.Endpoint, "bucket", s.Bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteBackupStore(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var n int
	_ = api.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM game_backups WHERE store_id = ?`, id).Scan(&n)
	if n > 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("it has %d backup%s in it: delete those first, so they aren't lost track of", n, map[bool]string{true: "", false: "s"}[n == 1]))
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM backup_stores WHERE id = ?`, id); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("backup storage removed", "user", u.Username, "store", id)
	w.WriteHeader(http.StatusNoContent)
}

// setBackupStore chooses where a game's new backups go: storeId 0 for its own server.
func (api *API) setBackupStore(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct {
		StoreID int64 `json:"storeId"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	var value any
	if body.StoreID != 0 {
		if _, err := api.store(r.Context(), body.StoreID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		value = body.StoreID
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE games SET backup_store_id = ? WHERE id = ?`, value, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game backup storage set", "user", u.Username, "game", g.ID, "store", body.StoreID)
	w.WriteHeader(http.StatusNoContent)
}

// s3Target is where a backup is in its store, for the agent; nil for one on the game's own server.
func (api *API) s3Target(ctx context.Context, storeID sql.NullInt64, gameID int64, file string) (*protocol.S3Target, error) {
	if !storeID.Valid {
		return nil, nil
	}
	s, err := api.store(ctx, storeID.Int64)
	if err != nil {
		return nil, err
	}
	return &protocol.S3Target{
		Endpoint: s.Endpoint, Region: s.Region, Bucket: s.Bucket, Key: s.Prefix + "game-" + strconv.FormatInt(gameID, 10) + "/" + file,
		AccessKey: s.AccessKey, SecretKey: s.SecretKey, PathStyle: s.PathStyle, PlainHTTP: s.PlainHTTP,
	}, nil
}
