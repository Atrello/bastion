package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

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

// A long file job runs on the panel: its progress shows in the list, and stopping it tells the agent to
// stop. Nobody else sees or stops it, and one person can't run more than a few at once.
func TestJobs(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	id, secret := b.joinAgent()
	ws, _ := b.dialAgent(id+"."+secret, nil)
	defer ws.Close()
	var mu sync.Mutex
	var cancelled []string
	go func() {
		for {
			var m protocol.Message
			if ws.ReadJSON(&m) != nil {
				return
			}
			ok := true
			switch {
			case m.Op == protocol.OpCancel:
				var a protocol.CancelArgs
				_ = json.Unmarshal(m.Args, &a)
				mu.Lock()
				cancelled = append(cancelled, a.ID)
				mu.Unlock()
				_ = ws.WriteJSON(protocol.Message{ID: m.ID, OK: &ok})
			case m.Op == protocol.OpGameFiles:
				// A long unpack: says how far it's got, then never answers until stopped.
				progress, _ := json.Marshal(protocol.Progress{ID: m.ID, Done: 40, Total: 100})
				_ = ws.WriteJSON(protocol.Message{Event: protocol.EventProgress, Data: progress})
			default:
				_ = ws.WriteJSON(protocol.Message{ID: m.ID, OK: &ok, Data: json.RawMessage(`{}`)})
			}
		}
	}()
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	if status, r := b.call(b.client, "POST", "/api/games", map[string]any{"name": "Survival", "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25565}}); status != 200 {
		t.Fatalf("making a game: %d %v", status, r)
	}

	status, started := b.call(b.client, "POST", "/api/games/1/files/unarchive", map[string]string{"path": "/mods/pack.zip"})
	if status != 202 || started["state"] != "running" || started["title"] != "Unpacking pack.zip" || started["detail"] != "Survival, /mods/" {
		t.Fatalf("starting: %d %v", status, started)
	}
	listJobs := func(client *http.Client) []map[string]any {
		res, err := client.Get(b.server.URL + "/api/jobs")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var list []map[string]any
		_ = json.NewDecoder(res.Body).Decode(&list)
		return list
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (len(listJobs(b.client)) != 1 || listJobs(b.client)[0]["done"] != float64(40)) {
		time.Sleep(20 * time.Millisecond)
	}
	if list := listJobs(b.client); len(list) != 1 || list[0]["done"] != float64(40) || list[0]["total"] != float64(100) {
		t.Fatalf("progress: %v", list)
	}
	if status, _ := b.call(b.newClient(), "DELETE", "/api/jobs/"+started["id"].(string), nil); status != 401 {
		t.Fatalf("stopped signed out: %d", status)
	}

	// Four more fill the limit; a sixth is refused.
	for i := 0; i < 4; i++ {
		b.call(b.client, "POST", "/api/games/1/files/unarchive", map[string]string{"path": "/other.zip"})
	}
	if status, _ := b.call(b.client, "POST", "/api/games/1/files/unarchive", map[string]string{"path": "/one-too-many.zip"}); status != 429 {
		t.Fatalf("a sixth job at once: %d", status)
	}

	if status, _ := b.call(b.client, "DELETE", "/api/jobs/"+started["id"].(string), nil); status != 204 {
		t.Fatalf("stopping: %d", status)
	}
	deadline = time.Now().Add(2 * time.Second)
	var state any
	for time.Now().Before(deadline) {
		for _, j := range listJobs(b.client) {
			if j["id"] == started["id"] {
				state = j["state"]
			}
		}
		mu.Lock()
		n := len(cancelled)
		mu.Unlock()
		if state == "stopped" && n == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if state != "stopped" || len(cancelled) != 1 {
		t.Fatalf("stopping: state %v, the agent told to stop %d times", state, len(cancelled))
	}
	// Cleared once it's ended.
	if status, _ := b.call(b.client, "DELETE", "/api/jobs/"+started["id"].(string), nil); status != 204 {
		t.Fatalf("clearing: %d", status)
	}
	if status, _ := b.call(b.client, "DELETE", "/api/jobs/"+started["id"].(string), nil); status != 404 {
		t.Fatalf("cleared twice: %d", status)
	}
}

// Game containers are named bastion-<server>-<game>, in letters Docker allows, and never the same as
// another game's on that server.
func TestGameContainerNames(t *testing.T) {
	for in, want := range map[string]string{
		"Survival SMP": "survival-smp", "  Créatif!! 2 ": "cr-atif-2", "valheim_v2.1": "valheim_v2.1", "!!!": "game",
		strings.Repeat("a", 60): strings.Repeat("a", 40),
	} {
		if got := dockerName(in, "game"); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	b := newPanel(t)
	b.setUp()
	fakeAgent(t, b)
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	for i, name := range []string{"Survival SMP", "survival smp!"} {
		if status, r := b.call(b.client, "POST", "/api/games", map[string]any{"name": name, "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25565 + i}}); status != 200 {
			t.Fatalf("making %q: %d %v", name, status, r)
		}
	}
	first, _ := b.api.loadGame(context.Background(), 1)
	second, _ := b.api.loadGame(context.Background(), 2)
	if a, c := b.api.containerName(context.Background(), first), b.api.containerName(context.Background(), second); a != "bastion-atlas-survival-smp-1" || c != "bastion-atlas-survival-smp-2" {
		t.Fatalf("names alike: %q and %q", a, c)
	}
}

// A friend can do only what their share allows; a schedule can't do for them what they couldn't by hand.
func TestSharePermissions(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	fakeAgent(t, b)
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	b.call(b.client, "POST", "/api/games", map[string]any{"name": "Survival", "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25565}})
	_, invite := b.call(b.client, "POST", "/api/games/1/shares", nil)
	token := invite["link"].(string)[strings.Index(invite["link"].(string), "#")+1:]
	friend := b.newClient()
	b.call(friend, "POST", "/api/setup", map[string]string{"token": token, "username": "jane", "password": "correct horse battery"})
	_, begun := b.call(friend, "POST", "/api/auth/authenticator/begin", map[string]string{"password": ""})
	code, _ := totp.GenerateCode(begun["secret"].(string), time.Now())
	if status, _ := b.call(friend, "POST", "/api/auth/authenticator/finish", map[string]string{"code": code}); status != 200 {
		t.Fatalf("friend's account: %d", status)
	}

	// Everything, until the owner chooses.
	if status, g := b.call(friend, "GET", "/api/games/1", nil); status != 200 || len(g["can"].([]any)) != len(gamePermissions) {
		t.Fatalf("a new share: %d %v", status, g["can"])
	}
	if status, r := b.call(b.client, "PUT", "/api/games/1/shares/2", map[string]any{"permissions": []string{"schedules", "console", "made-up"}}); status != 204 {
		t.Fatalf("choosing: %d %v", status, r)
	}
	_, g := b.call(friend, "GET", "/api/games/1", nil)
	if can := fmt.Sprint(g["can"]); can != "[console schedules]" {
		t.Fatalf("now can: %s", can)
	}
	for request, want := range map[string]int{
		"GET /api/games/1/console":         200,
		"POST /api/games/1/command":        204,
		"GET /api/games/1/history":         200,
		"GET /api/games/1/files?path=/":    403,
		"GET /api/games/1/backups":         403,
		"POST /api/games/1/backups":        403,
		"POST /api/games/1/power/stop":     403,
		"GET /api/games/1/startup":         403,
		"GET /api/games/1/schedules":       200,
		"PUT /api/games/1/shares/2":        403,
		"PUT /api/games/1/backup-store":    403,
		"DELETE /api/games/1/files?path=x": 403,
	} {
		method, path, _ := strings.Cut(request, " ")
		var body any
		if method == "POST" && strings.HasSuffix(path, "/command") {
			body = map[string]string{"command": "list"}
		}
		if status, r := b.call(friend, method, path, body); status != want {
			t.Errorf("%s: %d, want %d (%v)", request, status, want, r)
		}
	}
	// A schedule that runs a command is theirs to make; one that restarts or backs up isn't.
	schedule := map[string]any{"name": "Say hello", "when": "0 12 * * *", "action": "command", "payload": "say Hello", "enabled": true}
	if status, r := b.call(friend, "POST", "/api/games/1/schedules", schedule); status != 204 {
		t.Fatalf("a command schedule: %d %v", status, r)
	}
	for _, action := range []string{"restart", "backup"} {
		schedule["action"], schedule["payload"] = action, ""
		if status, _ := b.call(friend, "POST", "/api/games/1/schedules", schedule); status != 403 {
			t.Errorf("a friend without it made a %s schedule: %d", action, status)
		}
	}
	// The owner's restart schedule can't be run by them either.
	schedule["action"] = "restart"
	b.call(b.client, "POST", "/api/games/1/schedules", schedule)
	if status, _ := b.call(friend, "POST", "/api/games/1/schedules/2/run", nil); status != 403 {
		t.Errorf("a friend ran a restart schedule: %d", status)
	}
}

// A running game is asked who's playing, in the protocol its egg implies, and the answer shows on the game.
func TestPlayersOnline(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	id, secret := b.joinAgent()
	ws, _ := b.dialAgent(id+"."+secret, nil)
	defer ws.Close()
	asked := make(chan protocol.GameQueryArgs, 4)
	var writeMu sync.Mutex // one writer at a time on the agent's connection
	write := func(m protocol.Message) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.WriteJSON(m)
	}
	go func() {
		for {
			var m protocol.Message
			if ws.ReadJSON(&m) != nil {
				return
			}
			ok := true
			data := json.RawMessage(`{}`)
			if m.Op == protocol.OpGameQuery {
				var a protocol.GameQueryArgs
				_ = json.Unmarshal(m.Args, &a)
				asked <- a
				data = json.RawMessage(`{"online":3,"max":20,"names":["Alex","Jane Cooper"]}`)
			}
			write(protocol.Message{ID: m.ID, OK: &ok, Data: data})
		}
	}()
	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	b.call(b.client, "POST", "/api/games", map[string]any{"name": "Survival", "eggId": 1, "serverId": 1, "memoryMb": 1024, "ports": []int{25570}})
	metrics, _ := json.Marshal(protocol.Metrics{Games: []protocol.GameStatus{{ID: 1, State: "running"}}})
	write(protocol.Message{Event: protocol.EventMetrics, Data: metrics})
	deadline := time.Now().Add(2 * time.Second)
	for b.api.hub.Live(1).Metrics == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	b.api.checkGameEvents(context.Background())
	select {
	case a := <-asked:
		if a.Kind != "minecraft" || len(a.Ports) != 1 || a.Ports[0] != 25570 {
			t.Fatalf("asked %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the game wasn't asked")
	}
	_, g := b.call(b.client, "GET", "/api/games/1", nil)
	p, _ := g["players"].(map[string]any)
	if p == nil || p["online"] != float64(3) || p["max"] != float64(20) || len(p["names"].([]any)) != 2 {
		t.Fatalf("players: %v", g["players"])
	}
}
