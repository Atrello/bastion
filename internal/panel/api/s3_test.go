package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Atrello/bastion/internal/agent"
)

// fakeS3 is just enough of S3 for backups: put, get (with ranges), head and delete, in memory, path style.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=TESTKEY/") {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>InvalidAccessKeyId</Code></Error>`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.URL.Path
	switch r.Method {
	case http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		f.objects[key] = data
		sum := md5.Sum(data)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	case http.MethodHead, http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			}
			return
		}
		sum := md5.Sum(data)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		body := data
		if rg := r.Header.Get("Range"); rg != "" {
			var from, to int
			_, _ = fmt.Sscanf(rg, "bytes=%d-%d", &from, &to)
			body = data[from:min(to+1, len(data))]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, from+len(body)-1, len(data)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// Backup storage: saved only once it works, its secret never given back, and a game's backups made into
// it, downloaded, restored and deleted there, through the real agent.
func TestBackupsInS3(t *testing.T) {
	s3 := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(s3)
	defer srv.Close()
	endpoint := strings.TrimPrefix(srv.URL, "http://")

	b := newPanel(t)
	b.setUp()
	dir := t.TempDir()
	t.Setenv("BASTION_AGENT_CONFIG", filepath.Join(dir, "agent.json"))
	t.Setenv("BASTION_AGENT_GAMES_DIR", filepath.Join(dir, "games"))
	t.Setenv("BASTION_AGENT_BACKUPS_DIR", filepath.Join(dir, "backups"))
	id, secret := b.joinAgent()
	raw, _ := json.Marshal(agent.Config{Panel: b.server.URL, AgentID: id, Secret: secret, StacksDir: dir})
	_ = os.WriteFile(filepath.Join(dir, "agent.json"), raw, 0o600)
	cfg, _ := agent.LoadConfig()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go agent.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	store := map[string]any{"name": "Lab", "endpoint": "http://" + endpoint, "region": "us-east-1", "bucket": "game-backups", "prefix": "/bastion/",
		"accessKey": "WRONGKEY", "secretKey": "s3cret", "pathStyle": true}
	if status, r := b.call(b.client, "POST", "/api/backup-stores", store); status != 400 || !strings.Contains(r["error"].(string), "refused the keys") {
		t.Fatalf("wrong keys were saved: %d %v", status, r)
	}
	store["accessKey"] = "TESTKEY"
	if status, r := b.call(b.client, "POST", "/api/backup-stores", store); status != 204 {
		t.Fatalf("saving: %d %v", status, r)
	}
	res, _ := b.client.Get(b.server.URL + "/api/backup-stores")
	listed, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(listed), "s3cret") || !strings.Contains(string(listed), `"plainHttp":true`) || !strings.Contains(string(listed), `"prefix":"bastion/"`) {
		t.Fatalf("listed: %s", listed)
	}

	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	_, _ = b.api.db.Exec(`INSERT INTO games (id, name, egg_id, server_id, image, memory_mb, ports) VALUES (1, 'Survival', 1, 1, 'x', 1024, '[25565]')`)
	world := filepath.Join(dir, "games", "1", "world")
	_ = os.MkdirAll(world, 0o755)
	_ = os.WriteFile(filepath.Join(world, "level.dat"), []byte("the world as it was"), 0o644)
	deadline := time.Now().Add(5 * time.Second)
	for !b.api.hub.Live(1).Online && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if status, r := b.call(b.client, "PUT", "/api/games/1/backup-store", map[string]any{"storeId": 1}); status != 204 {
		t.Fatalf("choosing the storage: %d %v", status, r)
	}

	_, made := b.call(b.client, "POST", "/api/games/1/backups", map[string]string{"name": "Before the update"})
	backup := fmt.Sprintf("/api/games/1/backups/%v", made["id"])
	var state string
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = b.api.db.QueryRow(`SELECT state FROM game_backups WHERE id = ?`, made["id"]).Scan(&state)
		if state != "making" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s3.mu.Lock()
	var key string
	for k := range s3.objects {
		key = k
	}
	s3.mu.Unlock()
	if state != "done" || !strings.HasPrefix(key, "/game-backups/bastion/game-1/b-") {
		t.Fatalf("making it: %s, in S3 as %q", state, key)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "backups", "1", "*")); len(left) != 0 {
		t.Fatalf("a copy stayed on the server: %v", left)
	}

	res, _ = b.client.Get(b.server.URL + backup + "/download")
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	s3.mu.Lock()
	want := s3.objects[key]
	s3.mu.Unlock()
	if res.StatusCode != 200 || string(got) != string(want) {
		t.Fatalf("downloading: %d, %d bytes of %d", res.StatusCode, len(got), len(want))
	}

	_ = os.WriteFile(filepath.Join(world, "level.dat"), []byte("griefed"), 0o644)
	if status, r := b.call(b.client, "POST", backup+"/restore", nil); status != 204 {
		t.Fatalf("restoring: %d %v", status, r)
	}
	if raw, _ := os.ReadFile(filepath.Join(world, "level.dat")); string(raw) != "the world as it was" {
		t.Fatalf("restored: %q", raw)
	}

	if status, r := b.call(b.client, "DELETE", "/api/backup-stores/1", nil); status != 400 || !strings.Contains(r["error"].(string), "1 backup in it") {
		t.Fatalf("removing storage with a backup in it: %d %v", status, r)
	}
	store["bucket"] = "elsewhere"
	if status, _ := b.call(b.client, "PUT", "/api/backup-stores/1", store); status != 400 {
		t.Fatal("storage with backups in it was moved")
	}
	if status, r := b.call(b.client, "DELETE", backup, nil); status != 204 {
		t.Fatalf("deleting: %d %v", status, r)
	}
	s3.mu.Lock()
	n := len(s3.objects)
	s3.mu.Unlock()
	if n != 0 {
		t.Fatalf("still in S3: %d", n)
	}
	if status, _ := b.call(b.client, "DELETE", "/api/backup-stores/1", nil); status != 204 {
		t.Fatalf("removing empty storage: %d", status)
	}
}
