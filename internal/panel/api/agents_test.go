package api

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Atrello/bastion/internal/agent"
	"github.com/Atrello/bastion/internal/protocol"
	"github.com/gorilla/websocket"
)

// A fake agent: joins with the token from Add a server, connects, and answers like the real one would.
func (b *browser) joinAgent() (agentID, secret string) {
	b.t.Helper()
	_, added := b.call(b.client, "POST", "/api/servers", map[string]string{"name": "atlas"})
	command := added["joinCommand"].(string)
	token := command[strings.LastIndex(command, " ")+1:]
	status, reply := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": token, "hostname": "atlas"})
	if status != 200 {
		b.t.Fatalf("enrol: %d %v", status, reply)
	}
	// The token works once.
	if status, _ := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": token, "hostname": "evil"}); status != 401 {
		b.t.Fatalf("a join token worked twice: %d", status)
	}
	return reply["agentId"].(string), reply["secret"].(string)
}

func (b *browser) dialAgent(credentials string, header http.Header) (*websocket.Conn, int) {
	if header == nil {
		header = http.Header{}
	}
	header.Set("Authorization", "Bearer "+credentials)
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(b.server.URL, "http")+"/api/agent/connect", header)
	if err != nil {
		return nil, res.StatusCode
	}
	return ws, 101
}

func TestAgentsJoinAndAnswer(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	id, secret := b.joinAgent()

	if _, status := b.dialAgent(id+".wrong-secret", nil); status != 401 {
		t.Fatalf("an agent with the wrong secret connected: %d", status)
	}
	// A web page can't open the agents' connection, even with the secret.
	if _, status := b.dialAgent(id+"."+secret, http.Header{"Origin": {b.server.URL}}); status == 101 {
		t.Fatal("a browser opened the agents' connection")
	}

	ws, status := b.dialAgent(id+"."+secret, nil)
	if status != 101 {
		t.Fatalf("the agent couldn't connect: %d", status)
	}
	defer ws.Close()
	state, _ := json.Marshal(protocol.State{
		Stacks:     []protocol.Stack{{Name: "immich", ConfigFile: "/opt/stacks/immich/compose.yaml", Running: 2, Total: 3}},
		Containers: []protocol.Container{{ID: "abc", Name: "immich_server", State: "running", Stack: "immich"}},
	})
	_ = ws.WriteJSON(protocol.Message{Event: protocol.EventState, Data: state})

	// The panel shows what the agent reported.
	deadline := time.Now().Add(2 * time.Second)
	var stacks []map[string]any
	for time.Now().Before(deadline) {
		res, _ := b.client.Get(b.server.URL + "/api/stacks")
		_ = json.NewDecoder(res.Body).Decode(&stacks)
		res.Body.Close()
		if len(stacks) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(stacks) != 1 || stacks[0]["state"] != "partial" || stacks[0]["serverName"] != "atlas" {
		t.Fatalf("stacks: %v", stacks)
	}

	// A request goes to the agent, and its answer comes back.
	go func() {
		var m protocol.Message
		if ws.ReadJSON(&m) == nil && m.Op == protocol.OpStackAction {
			ok := true
			out, _ := json.Marshal(protocol.ActionResult{Output: "Container immich_server Started"})
			_ = ws.WriteJSON(protocol.Message{ID: m.ID, OK: &ok, Data: out})
		}
	}()
	status, result := b.call(b.client, "POST", "/api/servers/1/stacks/immich/actions/restart", nil)
	if status != 200 || result["output"] != "Container immich_server Started" {
		t.Fatalf("action: %d %v", status, result)
	}

	// Signed out, none of it is visible.
	if res, _ := b.newClient().Get(b.server.URL + "/api/stacks"); res.StatusCode != 401 {
		t.Fatalf("stacks were visible without signing in: %d", res.StatusCode)
	}
}

// A new join command doesn't cut off the agent a server has now; using it replaces that agent.
func TestNewJoinCommandKeepsTheAgentUntilUsed(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	id, secret := b.joinAgent()
	status, answer := b.call(b.client, "POST", "/api/servers/1/join-command", nil)
	if status != 200 {
		t.Fatalf("join command: %d", status)
	}
	if ws, code := b.dialAgent(id+"."+secret, nil); code != 101 {
		t.Fatalf("the old agent was cut off by a new join command: %d", code)
	} else {
		ws.Close()
	}
	command := answer["joinCommand"].(string)
	token := command[strings.LastIndex(command, " ")+1:]
	if status, _ := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": token, "hostname": "atlas"}); status != 200 {
		t.Fatalf("enrol with the new command: %d", status)
	}
	if _, code := b.dialAgent(id+"."+secret, nil); code != 401 {
		t.Fatalf("the replaced agent still connects: %d", code)
	}
}

// A server added but not yet joined isn't listed. Adding it again under the same name makes a new command
// and the old one stops working; once its agent joins, it's listed.
func TestServerListedOnceJoined(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	list := func() []map[string]any {
		res, err := b.client.Get(b.server.URL + "/api/servers")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var servers []map[string]any
		_ = json.NewDecoder(res.Body).Decode(&servers)
		return servers
	}
	tokenOf := func(added map[string]any) string {
		command := added["joinCommand"].(string)
		return command[strings.LastIndex(command, " ")+1:]
	}

	_, first := b.call(b.client, "POST", "/api/servers", map[string]string{"name": "vega"})
	if servers := list(); len(servers) != 0 {
		t.Fatalf("a server that hasn't joined is listed: %v", servers)
	}
	status, second := b.call(b.client, "POST", "/api/servers", map[string]string{"name": "vega"})
	if status != 200 {
		t.Fatalf("adding a waiting server again: %d %v", status, second)
	}
	if status, _ := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": tokenOf(first), "hostname": "vega"}); status != 401 {
		t.Fatalf("the replaced join command still worked: %d", status)
	}
	if status, reply := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": tokenOf(second), "hostname": "vega"}); status != 200 {
		t.Fatalf("enrol: %d %v", status, reply)
	}
	if servers := list(); len(servers) != 1 || servers[0]["name"] != "vega" {
		t.Fatalf("the joined server isn't listed: %v", servers)
	}
	// Once joined, the name is taken.
	if status, _ := b.call(b.client, "POST", "/api/servers", map[string]string{"name": "vega"}); status != 400 {
		t.Fatalf("a joined server's name was reused: %d", status)
	}
}

// An agent that isn't the one the panel hands out shows as needing an update, and is told which to fetch.
// One too old to know how is told to update by hand; one asked for something it doesn't know says so.
func TestAgentUpdate(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	program := []byte("the panel's agent for amd64")
	_ = os.WriteFile(filepath.Join(b.api.cfg.AgentBinaries, "bastion-agent-linux-amd64"), program, 0o755)
	sum := sha256.Sum256(program)
	want := hex.EncodeToString(sum[:])

	id, secret := b.joinAgent()
	ws, _ := b.dialAgent(id+"."+secret, nil)
	defer ws.Close()
	hello, _ := json.Marshal(protocol.Hello{Version: "0.3.0", Arch: "amd64", Build: "an-older-one"})
	_ = ws.WriteJSON(protocol.Message{Event: protocol.EventHello, Data: hello})

	update := func() []map[string]any {
		var servers []map[string]any
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			res, _ := b.client.Get(b.server.URL + "/api/servers")
			_ = json.NewDecoder(res.Body).Decode(&servers)
			res.Body.Close()
			if len(servers) == 1 && servers[0]["agentUpdate"] == true {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		return servers
	}
	if servers := update(); len(servers) != 1 || servers[0]["agentUpdate"] != true {
		t.Fatalf("an old agent isn't shown as needing an update: %v", servers)
	}

	// The fake agent: first answers like an agent from before updates, then like a current one.
	asked := make(chan protocol.AgentUpdateArgs, 2)
	go func() {
		for i := 0; ; i++ {
			var m protocol.Message
			if ws.ReadJSON(&m) != nil {
				return
			}
			if m.Op != protocol.OpAgentUpdate {
				continue
			}
			var a protocol.AgentUpdateArgs
			_ = json.Unmarshal(m.Args, &a)
			asked <- a
			ok := i > 0
			reply := protocol.Message{ID: m.ID, OK: &ok}
			if !ok {
				reply.Error = "unknown request agent.update"
			}
			_ = ws.WriteJSON(reply)
		}
	}()
	if status, r := b.call(b.client, "POST", "/api/servers/1/agent/update", nil); status != 400 || !strings.Contains(r["error"].(string), "by hand") {
		t.Fatalf("an agent too old to update itself: %d %v", status, r)
	}
	<-asked
	if status, r := b.call(b.client, "POST", "/api/servers/1/agent/update", nil); status != 204 {
		t.Fatalf("updating: %d %v", status, r)
	}
	if a := <-asked; a.Build != want {
		t.Fatalf("the agent was told to fetch %q, not the panel's %q", a.Build, want)
	}
	// Friends can't.
	if status, _ := b.call(b.newClient(), "POST", "/api/servers/1/agent/update", nil); status != 401 {
		t.Fatalf("updating signed out: %d", status)
	}
}

// A server joined again while its agent runs (a new join command run on it): the running agent finds the
// new credentials in its settings and reconnects with them, rather than being refused for good.
func TestAgentRejoinedWhileRunning(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	dir := t.TempDir()
	t.Setenv("BASTION_AGENT_CONFIG", filepath.Join(dir, "agent.json"))
	t.Setenv("BASTION_AGENT_GAMES_DIR", filepath.Join(dir, "games"))
	write := func(id, secret string) {
		raw, _ := json.Marshal(agent.Config{Panel: b.server.URL, AgentID: id, Secret: secret, StacksDir: dir})
		_ = os.WriteFile(filepath.Join(dir, "agent.json"), raw, 0o600)
	}
	online := func(want bool) bool {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			var servers []map[string]any
			res, _ := b.client.Get(b.server.URL + "/api/servers")
			_ = json.NewDecoder(res.Body).Decode(&servers)
			res.Body.Close()
			if len(servers) == 1 && servers[0]["online"] == want {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}

	id, secret := b.joinAgent()
	write(id, secret)
	cfg, _ := agent.LoadConfig()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go agent.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !online(true) {
		t.Fatal("the agent didn't connect")
	}

	_, made := b.call(b.client, "POST", "/api/servers/1/join-command", nil)
	command := made["joinCommand"].(string)
	token := command[strings.LastIndex(command, " ")+1:]
	status, reply := b.call(b.newClient(), "POST", "/api/agent/enrol", map[string]string{"token": token, "hostname": "atlas"})
	if status != 200 {
		t.Fatalf("joining again: %d", status)
	}
	write(reply["agentId"].(string), reply["secret"].(string))
	time.Sleep(200 * time.Millisecond)
	if !online(true) {
		t.Fatal("the running agent didn't pick up its new credentials")
	}
}

// The whole way, with the real agent: unpacking a game's archive is a job whose progress comes back from
// the agent, and stopping it stops the agent's unpacking, which says what it left.
func TestUnpackJobWithRealAgent(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	dir := t.TempDir()
	t.Setenv("BASTION_AGENT_CONFIG", filepath.Join(dir, "agent.json"))
	t.Setenv("BASTION_AGENT_GAMES_DIR", filepath.Join(dir, "games"))
	id, secret := b.joinAgent()
	raw, _ := json.Marshal(agent.Config{Panel: b.server.URL, AgentID: id, Secret: secret, StacksDir: dir})
	_ = os.WriteFile(filepath.Join(dir, "agent.json"), raw, 0o600)
	cfg, _ := agent.LoadConfig()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go agent.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	paper, _ := os.ReadFile("../eggs/testdata/egg-paper.json")
	b.call(b.client, "POST", "/api/eggs", map[string]any{"json": string(paper)})
	_, _ = b.api.db.Exec(`INSERT INTO games (id, name, egg_id, server_id, image, memory_mb, ports) VALUES (1, 'Survival', 1, 1, 'x', 1024, '[25565]')`)
	// A tar of incompressible data: the agent reads it slowly enough to be stopped part-way.
	_ = os.MkdirAll(filepath.Join(dir, "games", "1"), 0o755)
	f, _ := os.Create(filepath.Join(dir, "games", "1", "world.tar"))
	tw := tar.NewWriter(f)
	chunk := make([]byte, 8<<20)
	for i := 0; i < 64; i++ {
		_, _ = rand.Read(chunk)
		_ = tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("world/r.%d.mca", i), Mode: 0o644, Size: int64(len(chunk)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(chunk)
	}
	_ = tw.Close()
	_ = f.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !b.api.hub.Live(1).Online && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	status, job := b.call(b.client, "POST", "/api/games/1/files/unarchive", map[string]string{"path": "/world.tar"})
	if status != 202 {
		t.Fatalf("starting: %d %v", status, job)
	}
	jobNow := func() map[string]any {
		res, err := b.client.Get(b.server.URL + "/api/jobs")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var list []map[string]any
		_ = json.NewDecoder(res.Body).Decode(&list)
		for _, j := range list {
			if j["id"] == job["id"] {
				return j
			}
		}
		return nil
	}
	// Progress arrives from the agent.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if j := jobNow(); j["total"].(float64) > 0 || j["state"] != "running" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	j := jobNow()
	if j["state"] == "running" {
		if j["total"].(float64) == 0 {
			t.Fatalf("no progress came: %v", j)
		}
		b.call(b.client, "DELETE", "/api/jobs/"+job["id"].(string), nil)
		deadline = time.Now().Add(10 * time.Second)
		for jobNow()["state"] != "stopped" && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if j := jobNow(); j["state"] != "stopped" || j["error"] != "Stopped: what was unpacked so far stays." {
			t.Fatalf("not stopped: %v", j)
		}
		if _, err := os.Stat(filepath.Join(dir, "games", "1", "world", "r.63.mca")); err == nil {
			t.Fatal("it unpacked to the end though stopped")
		}
	} else {
		t.Logf("it finished before it could be stopped: %v", j)
	}
}
