package api

import (
	"encoding/json"
	"net/http"
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
