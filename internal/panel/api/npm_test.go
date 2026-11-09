package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeNPM stands in for Nginx Proxy Manager's API: one user, one proxy host, and a record of changes.
func fakeNPM(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var changes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tokens" {
			var body struct{ Identity, Secret string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Identity != "robin@example.com" || body.Secret != "npm-password" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"Invalid email or password"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"token":"t1","expires":"2099-01-01T00:00:00Z"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/nginx/proxy-hosts" {
			_, _ = w.Write([]byte(`[{"id":1,"domain_names":["jellyfin.home.lab"],"forward_host":"10.0.0.5","forward_port":8096,"enabled":1}]`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		changes = append(changes, r.Method+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":2}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &changes
}

func TestNPM(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	npmServer, changes := fakeNPM(t)

	if status, _ := b.call(b.client, "POST", "/api/npm", map[string]string{"name": "Proxy", "url": npmServer.URL, "email": "robin@example.com", "password": "wrong"}); status != 400 {
		t.Fatalf("wrong password linked: %d", status)
	}
	if status, _ := b.call(b.client, "POST", "/api/npm", map[string]string{"url": "file:///etc/passwd", "email": "robin@example.com", "password": "npm-password"}); status != 400 {
		t.Fatalf("a file address was accepted: %d", status)
	}
	if status, answer := b.call(b.client, "POST", "/api/npm", map[string]string{"name": "Proxy", "url": npmServer.URL + "/", "email": "robin@example.com", "password": "npm-password"}); status != 204 {
		t.Fatalf("linking: %d %v", status, answer)
	}

	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", b.server.URL+path, nil)
		res, err := b.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	if _, list := get("/api/npm"); strings.Contains(list, "npm-password") || !strings.Contains(list, `"name":"Proxy"`) {
		t.Fatalf("instances: %s", list)
	}
	if status, hosts := get("/api/npm/1/proxy-hosts"); status != 200 || !strings.Contains(hosts, "jellyfin.home.lab") {
		t.Fatalf("hosts: %d %s", status, hosts)
	}
	if status, _ := get("/api/npm/1/settings"); status != 404 {
		t.Fatalf("an unknown kind was passed through: %d", status)
	}

	if status, _ := b.call(b.client, "PUT", "/api/npm/1/proxy-hosts/1", map[string]any{"forward_port": 8097}); status != 200 {
		t.Fatalf("updating: %d", status)
	}
	if status, _ := b.call(b.client, "POST", "/api/npm/1/proxy-hosts/1/disable", nil); status != 200 {
		t.Fatalf("disabling: %d", status)
	}
	for _, path := range []string{"/api/npm/1/proxy-hosts/1/renew", "/api/npm/1/proxy-hosts/..%2F..%2Fusers/enable", "/api/npm/1/access-lists/1/enable"} {
		if status, _ := b.call(b.client, "POST", path, nil); status != 404 {
			t.Fatalf("%s: %d", path, status)
		}
	}
	if len(*changes) != 2 || !strings.HasPrefix((*changes)[0], `PUT /api/nginx/proxy-hosts/1 {"forward_port":8097}`) ||
		!strings.HasPrefix((*changes)[1], "POST /api/nginx/proxy-hosts/1/disable") {
		t.Fatalf("NPM got %q", *changes)
	}

	// Signed out, nothing gets through.
	req, _ := http.NewRequest("GET", b.server.URL+"/api/npm/1/proxy-hosts", nil)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Fatalf("signed out: %d", res.StatusCode)
	}
}
