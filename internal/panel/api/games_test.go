package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/protocol"
)

// fakeAgent answers every request with OK, keeping what it was asked: asked() gives a copy so far.
func fakeAgent(t *testing.T, b *browser) (asked func() []protocol.Message) {
	id, secret := b.joinAgent()
	ws, _ := b.dialAgent(id+"."+secret, nil)
	t.Cleanup(func() { ws.Close() })
	var mu sync.Mutex
	var got []protocol.Message
	go func() {
		for {
			var m protocol.Message
			if ws.ReadJSON(&m) != nil {
				return
			}
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
			ok := true
			data := json.RawMessage(`{}`)
			if m.Op == protocol.OpStackLogs {
				data = json.RawMessage(`{"lines":["Done (3.2s)! For help, type \"help\""]}`)
			}
			_ = ws.WriteJSON(protocol.Message{ID: m.ID, OK: &ok, Data: data})
		}
	}()
	time.Sleep(100 * time.Millisecond)
	return func() []protocol.Message {
		mu.Lock()
		defer mu.Unlock()
		return append([]protocol.Message(nil), got...)
	}
}

func TestGamesAndSharing(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	asked := fakeAgent(t, b)

	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	if status, r := b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper), "category": "Minecraft"}); status != 204 {
		t.Fatalf("importing: %d %v", status, r)
	}
	// The same egg again is only replaced when asked.
	if status, r := b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)}); status != 409 || r["existing"] != float64(1) {
		t.Fatalf("a second import: %d %v", status, r)
	}
	if status, _ := b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper), "replace": true}); status != 204 {
		t.Fatalf("replacing: %d", status)
	}
	if status, _ := b.call(b.client, "POST", "/api/eggs", map[string]any{"url": "http://example.com/egg.json"}); status != 400 {
		t.Fatalf("a plain http link was fetched: %d", status)
	}

	newGame := map[string]any{"name": "Survival", "eggId": 1, "serverId": 1, "memoryMb": 4096, "ports": []int{25565}}
	if status, r := b.call(b.client, "POST", "/api/games", newGame); status != 200 || r["state"] == nil {
		t.Fatalf("making a game: %d %v", status, r)
	}
	if status, r := b.call(b.client, "POST", "/api/games", map[string]any{"name": "Creative", "eggId": 1, "serverId": 1, "memoryMb": 2048, "ports": []int{25565}}); status != 400 || !strings.Contains(r["error"].(string), "Survival") {
		t.Fatalf("a port used twice: %d %v", status, r)
	}
	if status, _ := b.call(b.client, "POST", "/api/games", map[string]any{"name": "Creative", "eggId": 1, "serverId": 1, "memoryMb": 2048, "ports": []int{25566}}); status != 200 {
		t.Fatal("making a second game")
	}
	var install protocol.GameSpec
	for _, m := range asked() {
		if m.Op == protocol.OpGameInstall {
			_ = json.Unmarshal(m.Args, &install)
			break
		}
	}
	if install.Image != "ghcr.io/pterodactyl/yolks:java_25" || !strings.Contains(install.Startup, "-jar server.jar") ||
		len(install.ConfigFiles) != 1 || install.ConfigFiles[0].Find["server-port"] != "25565" || install.InstallScript == "" {
		t.Fatalf("install spec: %+v", install)
	}

	// Settings are checked against the egg's rules.
	if status, _ := b.call(b.client, "PUT", "/api/games/1/startup", map[string]string{"SERVER_JARFILE": "server.sh"}); status != 400 {
		t.Fatalf("a jar file that isn't a .jar was accepted: %d", status)
	}

	// A friend, invited to Survival, sees only it.
	status, invite := b.call(b.client, "POST", "/api/games/1/shares", nil)
	if status != 200 {
		t.Fatalf("invite: %d", status)
	}
	token := invite["link"].(string)[strings.Index(invite["link"].(string), "#")+1:]
	friend := b.newClient()
	if status, r := b.call(friend, "POST", "/api/setup", map[string]string{"token": token, "username": "jane", "password": "correct horse battery"}); status != 204 {
		t.Fatalf("friend setup: %d %v", status, r)
	}
	_, begun := b.call(friend, "POST", "/api/auth/authenticator/begin", map[string]string{"password": ""})
	code, _ := totp.GenerateCode(begun["secret"].(string), time.Now())
	if status, _ := b.call(friend, "POST", "/api/auth/authenticator/finish", map[string]string{"code": code}); status != 200 {
		t.Fatalf("friend authenticator: %d", status)
	}
	if status, _ := b.call(b.newClient(), "POST", "/api/setup", map[string]string{"token": token, "username": "mallory", "password": "correct horse battery"}); status != 400 {
		t.Fatal("an invite link worked twice")
	}

	for path, want := range map[string]int{
		"/api/games/1":              200,
		"/api/games/1/files?path=/": 200,
		"/api/games/1/backups":      200,
		"/api/games/2":              404, // not shared with her
		"/api/games/2/files?path=/": 404,
		"/api/servers":              403,
		"/api/stacks":               403,
		"/api/eggs":                 403,
		"/api/npm":                  403,
		"/api/alerts":               403,
		"/api/games/1/shares":       403,
		"/api/account":              200,
	} {
		if status, _ := b.call(friend, "GET", path, nil); status != want {
			t.Errorf("friend GET %s: %d, want %d", path, status, want)
		}
	}
	if status, _ := b.call(friend, "DELETE", "/api/games/1", nil); status != 403 {
		t.Errorf("a friend deleted the game: %d", status)
	}
	if status, _ := b.call(friend, "PUT", "/api/games/1", map[string]any{"name": "Mine", "memoryMb": 65536, "ports": []int{25565}}); status != 403 {
		t.Errorf("a friend changed the game's limits: %d", status)
	}
	if status, _ := b.call(friend, "POST", "/api/games/1/power/start", nil); status != 204 {
		t.Errorf("a friend couldn't start the game: %d", status)
	}
	// Paper's version is one players may change; its download address isn't.
	if status, _ := b.call(friend, "PUT", "/api/games/1/startup", map[string]string{"DL_PATH": "https://example.com/evil.jar"}); status != 403 {
		t.Errorf("a friend changed an owner-only setting: %d", status)
	}
	if status, _ := b.call(friend, "PUT", "/api/games/1/startup", map[string]string{"MINECRAFT_VERSION": "1.21.4"}); status != 204 {
		t.Errorf("a friend couldn't change the version: %d", status)
	}

	// Unshared, she sees nothing.
	if status, _ := b.call(b.client, "DELETE", "/api/games/1/shares/2", nil); status != 204 {
		t.Fatal("unsharing")
	}
	if status, _ := b.call(friend, "GET", "/api/games/1", nil); status != 404 {
		t.Errorf("unshared, the friend still sees it: %d", status)
	}
}

// An upload can't take a game past its disk limit: refused up front when its size is given, and removed
// when it isn't and runs over.
func TestUploadKeepsToDiskLimit(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	asked := fakeAgent(t, b)
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	if status, r := b.call(b.client, "POST", "/api/games", map[string]any{"name": "Small", "eggId": 1, "serverId": 1, "memoryMb": 1024, "diskMb": 3, "ports": []int{25565}}); status != 200 {
		t.Fatalf("making a game: %d %v", status, r)
	}
	upload := func(body io.Reader) int {
		req, _ := http.NewRequest("POST", b.server.URL+"/api/games/1/files/upload?path=/world.zip", body)
		req.Header.Set("Origin", b.server.URL)
		res, err := b.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	ops := func(op string) (n int) {
		for _, m := range asked() {
			var a protocol.GameFileArgs
			if m.Op == protocol.OpGameFiles && json.Unmarshal(m.Args, &a) == nil && a.Op == op {
				n++
			}
		}
		return n
	}

	if status := upload(bytes.NewReader(make([]byte, 4<<20))); status != 413 || ops("append") != 0 {
		t.Fatalf("an upload over the disk limit, size given: %d, %d pieces sent", status, ops("append"))
	}
	// A reader of unknown size is sent without a length.
	if status := upload(io.MultiReader(bytes.NewReader(make([]byte, 4<<20)))); status != 413 || ops("delete") != 1 {
		t.Fatalf("an upload over the disk limit, size not given: %d, %d deletes", status, ops("delete"))
	}
	if status := upload(bytes.NewReader(make([]byte, 1<<20))); status != 204 {
		t.Fatalf("an upload within the limit: %d", status)
	}

	// Cut off partway (the tab closed, or cancelled): what came is removed, not kept as the whole file.
	req, _ := http.NewRequest("POST", b.server.URL+"/api/games/1/files/upload?path=/world.zip",
		io.MultiReader(bytes.NewReader(make([]byte, 1<<20)), failingReader{}))
	req.ContentLength = 2 << 20
	req.Header.Set("Origin", b.server.URL)
	if res, err := b.client.Do(req); err == nil {
		res.Body.Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for ops("delete") < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ops("delete") != 2 {
		t.Fatal("a cut-off upload was kept")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("the tab closed") }

// A long job keeps its request open with spaces, then gives its outcome, so a proxy in front doesn't give up.
func TestSlowJobsKeepTalking(t *testing.T) {
	slowAfter, keepAlive = 50*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { slowAfter, keepAlive = 5*time.Second, 15*time.Second })
	b := newPanel(t)
	answer := func(job func(context.Context) error) (*http.Response, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)
		b.api.slowly(rec, req, job)
		return rec.Result(), rec.Body.String()
	}
	if res, _ := answer(func(context.Context) error { return nil }); res.StatusCode != 204 {
		t.Fatalf("a quick job: %d", res.StatusCode)
	}
	res, body := answer(func(context.Context) error { time.Sleep(150 * time.Millisecond); return nil })
	if res.StatusCode != 200 || res.Header.Get("X-Bastion-Slow") != "1" || !strings.HasPrefix(body, "  ") || strings.TrimSpace(body) != "{}" {
		t.Fatalf("a slow job: %d %q", res.StatusCode, body)
	}
	_, body = answer(func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return &agents.AgentError{Message: "that zip can't be read"}
	})
	var outcome struct {
		Error  string
		Status int
	}
	if err := json.Unmarshal([]byte(body), &outcome); err != nil || outcome.Error != "that zip can't be read" || outcome.Status != 400 {
		t.Fatalf("a slow job that failed: %q", body)
	}
}
