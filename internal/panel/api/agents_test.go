package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
