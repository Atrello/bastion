package api

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// What people do is kept with who did it, named rather than numbered; failed sign-ins too. Only the owner
// can read it.
func TestActivity(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	fakeAgent(t, b)
	b.call(b.client, "POST", "/api/servers/1/join-command", nil)
	b.call(b.newClient(), "POST", "/api/auth/signin", map[string]string{"username": "robin", "password": "not it"})

	list := func(query string) (entries []map[string]any) {
		res, err := b.client.Get(b.server.URL + "/api/activity" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var page struct{ Entries []map[string]any }
		_ = json.Unmarshal(raw, &page)
		return page.Entries
	}
	find := func(entries []map[string]any, action string) map[string]any {
		for _, e := range entries {
			if e["action"] == action {
				return e
			}
		}
		return nil
	}
	all := list("")
	made := find(all, "new join command made")
	if made == nil || made["user"] != "robin" || made["ok"] != true || made["details"].(map[string]any)["server"] != "atlas" {
		t.Fatalf("the join command's entry: %v (all: %v)", made, all)
	}
	if failed := find(all, "sign-in failed: wrong username or password"); failed == nil || failed["ok"] != false {
		t.Fatalf("a failed sign-in wasn't kept: %v", all)
	}
	if got := list("?q=join"); len(got) != 1 {
		t.Fatalf("searching: %v", got)
	}
	if got := list("?user=nobody"); len(got) != 0 {
		t.Fatalf("one person's: %v", got)
	}
	// Signed out, nothing.
	if status, _ := b.call(b.newClient(), "GET", "/api/activity", nil); status != 401 {
		t.Fatalf("read signed out: %d", status)
	}
	if strings.Contains(string(mustJSON(all)), "not it") {
		t.Fatal("a password was kept")
	}
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

// The health check answers signed out, and only says whether the panel works.
func TestHealth(t *testing.T) {
	b := newPanel(t)
	res, err := b.newClient().Get(b.server.URL + "/api/health")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("health: %v %v", err, res)
	}
	res.Body.Close()
}
