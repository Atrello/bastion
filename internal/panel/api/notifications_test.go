package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A notification for one game's backups, in its own words, with a download link that works signed out
// until it's stopped; others for state changes.
func TestNotifications(t *testing.T) {
	type sent struct{ title, body, click string }
	var mu sync.Mutex
	var got []sent
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, sent{r.Header.Get("Title"), string(body), r.Header.Get("Click")})
		mu.Unlock()
	}))
	defer ntfy.Close()

	b := newPanel(t)
	b.setUp()
	fakeAgent(t, b)
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	for i, name := range []string{"Survival SMP", "Creative"} {
		b.call(b.client, "POST", "/api/games", map[string]any{"name": name, "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25565 + i}})
	}
	b.call(b.client, "POST", "/api/alerts/channels", map[string]any{"kind": "ntfy", "name": "Discord-ish", "config": map[string]string{"url": ntfy.URL, "topic": "games"}})

	rule := map[string]any{"name": "Survival backups", "event": "backup_done", "gameId": 1, "channelId": 1, "enabled": true,
		"title": "{game} saved", "message": "\"{backup}\" ({size}) on {server}: {link}", "linkDays": 7}
	if status, r := b.call(b.client, "POST", "/api/notifications", rule); status != 204 {
		t.Fatalf("saving: %d %v", status, r)
	}
	bad := map[string]any{"name": "Up", "event": "game_started", "channelId": 1, "enabled": true, "title": "x", "message": "y", "linkDays": 3}
	if status, _ := b.call(b.client, "POST", "/api/notifications", bad); status != 400 {
		t.Fatal("a download link was allowed on a game starting")
	}

	// Creative's backup sends nothing; Survival's sends the message with a link.
	b.call(b.client, "POST", "/api/games/2/backups", map[string]string{"name": "Nightly"})
	b.call(b.client, "POST", "/api/games/1/backups", map[string]string{"name": "Before the update"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	messages := append([]sent(nil), got...)
	mu.Unlock()
	if len(messages) != 1 || messages[0].title != "Survival SMP saved" || !strings.HasPrefix(messages[0].body, `"Before the update-`+time.Now().Format("2006-01-02")) {
		t.Fatalf("sent: %+v", messages)
	}
	link := messages[0].click
	if !strings.Contains(messages[0].body, link) || !strings.Contains(link, "/api/backup-links/") {
		t.Fatalf("the link: %q in %q", link, messages[0].body)
	}

	// The link works signed out, until it's stopped.
	path := link[strings.Index(link, "/api/"):]
	res, err := b.newClient().Get(b.server.URL + path)
	if err != nil || res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), "survival-smp-before-the-update-20") {
		t.Fatalf("downloading by link: %v %v", err, res)
	}
	res.Body.Close()
	if status, _ := b.call(b.client, "DELETE", "/api/games/1/backups/2/links", nil); status != 204 {
		t.Fatalf("stopping links: %d", status)
	}
	if res, _ := b.newClient().Get(b.server.URL + path); res.StatusCode != 404 {
		t.Fatalf("a stopped link still works: %d", res.StatusCode)
	}
	if res, _ := b.newClient().Get(b.server.URL + "/api/backup-links/guessed"); res.StatusCode != 404 {
		t.Fatal("a made-up link worked")
	}
}

func TestGameEvents(t *testing.T) {
	for _, c := range []struct{ before, after, want string }{
		{"stopped", "starting", ""}, {"starting", "running", "game_started"}, {"stopped", "running", "game_started"},
		{"running", "stopped", "game_stopped"}, {"running", "crashed", "game_crashed"}, {"installing", "stopped", ""},
		{"crashed", "stopped", ""},
	} {
		if got := gameEvent(c.before, c.after); got != c.want {
			t.Errorf("%s to %s: %q, want %q", c.before, c.after, got, c.want)
		}
	}
	if got := fill("{game} on {server}: {nope}", map[string]string{"game": "Survival", "server": "atlas"}); got != "Survival on atlas: {nope}" {
		t.Errorf("fill: %q", got)
	}
}

// A backup's name has the date and time, and its file the same in plain letters.
func TestBackupNames(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	fakeAgent(t, b)
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	b.call(b.client, "POST", "/api/games", map[string]any{"name": "Survival SMP", "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25565}})
	_, made := b.call(b.client, "POST", "/api/games/1/backups", map[string]string{"name": "Before 1.21.4 update!"})
	stamp := time.Now().Format("2006-01-02")
	var file string
	_ = b.api.db.QueryRow(`SELECT file FROM game_backups WHERE id = ?`, made["id"]).Scan(&file)
	if name, _ := made["name"].(string); !strings.HasPrefix(name, "Before 1.21.4 update!-"+stamp) ||
		!strings.HasPrefix(file, "before-1-21-4-update-"+stamp) || !strings.HasSuffix(file, ".tar.gz") {
		t.Fatalf("named %q, file %q", made["name"], file)
	}
	_, unnamed := b.call(b.client, "POST", "/api/games/1/backups", nil)
	if name, _ := unnamed["name"].(string); !strings.HasPrefix(name, "Backup-"+stamp) {
		t.Fatalf("no name given: %q", name)
	}
}
