package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

func TestAlertsAreSentOnceAndResolved(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	id, secret := b.joinAgent()

	// A stand-in ntfy server that records what it's sent.
	var mu sync.Mutex
	var got []string
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Header.Get("Title")+" | "+string(body))
		mu.Unlock()
	}))
	defer ntfy.Close()
	if status, answer := b.call(b.client, "POST", "/api/alerts/channels", map[string]any{"kind": "ntfy", "name": "Phone", "config": map[string]string{"url": ntfy.URL, "topic": "homelab"}}); status != 204 {
		t.Fatalf("adding ntfy: %d %v", status, answer)
	}

	ws, _ := b.dialAgent(id+"."+secret, nil)
	defer ws.Close()
	report := func(state string) {
		raw, _ := json.Marshal(protocol.State{Containers: []protocol.Container{{ID: "a", Name: "paperless-worker-1", State: state, Status: "Restarting (1) 3 seconds ago"}}})
		_ = ws.WriteJSON(protocol.Message{Event: protocol.EventState, Data: raw})
		time.Sleep(100 * time.Millisecond)
	}

	report("restarting")
	b.api.checkAlerts(context.Background())
	b.api.checkAlerts(context.Background()) // still restarting: not sent again
	report("running")
	b.api.checkAlerts(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0][:31] != "paperless-worker-1 keeps restar" || got[1][:10] != "Resolved: " {
		t.Fatalf("ntfy got %q", got)
	}
	// The channel's settings never come back to the browser with secrets in them.
	_, page := b.call(b.client, "GET", "/api/alerts", nil)
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), `"token"`) || strings.Contains(string(raw), `"password"`) {
		t.Fatalf("secrets in the alerts page: %s", raw)
	}
}
