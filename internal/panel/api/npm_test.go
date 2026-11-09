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
	return fakeNPMWith(t, httptest.NewServer)
}

func fakeNPMWith(t *testing.T, start func(http.Handler) *httptest.Server) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var changes []string
	srv := start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			_, _ = w.Write([]byte(`[{"id":1,"domain_names":["jellyfin.home.lab"],"forward_host":"10.0.0.5","forward_port":8096,"enabled":1,
				"certificate":{"id":3,"meta":{"certificate_key":"-----BEGIN PRIVATE KEY-----","dns_provider_credentials":"dns_cloudflare_api_token = secret"}}}]`))
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
	if status, hosts := get("/api/npm/1/proxy-hosts"); status != 200 || !strings.Contains(hosts, "jellyfin.home.lab") ||
		strings.Contains(hosts, "PRIVATE KEY") || strings.Contains(hosts, "dns_cloudflare") {
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

// An NPM with a self-signed certificate is only linked by trusting that exact certificate.
func TestNPMSelfSigned(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	srv, _ := fakeNPMWith(t, httptest.NewTLSServer)
	link := map[string]string{"name": "Proxy", "url": srv.URL, "email": "robin@example.com", "password": "npm-password"}

	status, answer := b.call(b.client, "POST", "/api/npm", link)
	cert, _ := answer["certificate"].(map[string]any)
	if status != 409 || cert == nil || len(cert["fingerprint"].(string)) != 95 {
		t.Fatalf("an untrusted certificate: %d %v", status, answer)
	}
	wrong := "00:"
	if strings.HasPrefix(cert["fingerprint"].(string), "00") {
		wrong = "11:"
	}
	link["trustCertificate"] = wrong + cert["fingerprint"].(string)[3:]
	if status, _ := b.call(b.client, "POST", "/api/npm", link); status != 400 {
		t.Fatalf("a different certificate was trusted: %d", status)
	}
	link["trustCertificate"] = cert["fingerprint"].(string)
	if status, answer := b.call(b.client, "POST", "/api/npm", link); status != 204 {
		t.Fatalf("trusting the certificate: %d %v", status, answer)
	}
	req, _ := http.NewRequest("GET", b.server.URL+"/api/npm/1/proxy-hosts", nil)
	res, err := b.client.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("using the trusted NPM: %v %v", res, err)
	}
}

// When NPM can't be reached, it's said in words.
func TestNPMUnreachableInWords(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()
	for _, c := range []struct{ url, want string }{
		{address, "nothing answered there"},
		{strings.Replace(plain.URL, "http://", "https://", 1), "use http://"},
	} {
		_, answer := b.call(b.client, "POST", "/api/npm", map[string]string{"url": c.url, "email": "robin@example.com", "password": "x"})
		if msg, _ := answer["error"].(string); !strings.Contains(msg, c.want) {
			t.Errorf("%s: %q, want it to say %q", c.url, msg, c.want)
		}
	}
}

// NPMplus signs in with a cookie, not a token in its answer; Bastion works with either.
func TestNPMplus(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tokens" {
			http.SetCookie(w, &http.Cookie{Name: "__Host-Http-token", Value: "s%3Asigned.token", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"requiresTotp":false,"expires":"2099-01-01T00:00:00Z"}`))
			return
		}
		if ck, err := r.Cookie("__Host-Http-token"); err != nil || ck.Value != "s%3Asigned.token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"Permission Denied"}}`))
			return
		}
		seen = append(seen, r.URL.RawQuery)
		_, _ = w.Write([]byte(`[{"id":1,"domain_names":["jellyfin.home.lab"],"enabled":true,"npmplus_dns_provider_credentials":"secret"}]`))
	}))
	defer srv.Close()

	if status, answer := b.call(b.client, "POST", "/api/npm", map[string]string{"url": srv.URL, "email": "robin@example.com", "password": "x"}); status != 204 {
		t.Fatalf("linking NPMplus: %d %v", status, answer)
	}
	req, _ := http.NewRequest("GET", b.server.URL+"/api/npm", nil)
	res, _ := b.client.Do(req)
	raw, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(raw), `"flavour":"npmplus"`) {
		t.Fatalf("not seen as NPMplus: %s", raw)
	}
	req, _ = http.NewRequest("GET", b.server.URL+"/api/npm/1/proxy-hosts", nil)
	res, _ = b.client.Do(req)
	raw, _ = io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(raw), "secret") || !strings.Contains(seen[len(seen)-1], "access_lists") {
		t.Fatalf("listing NPMplus hosts: %d %s (asked %q)", res.StatusCode, raw, seen)
	}
}

// An NPM that fails a list asked to include something it doesn't know ("Internal Error", as some NPMplus
// versions do for access lists) still lists, with less included; the next time it's asked for that at once.
func TestNPMListWithoutUnknownExpand(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tokens" {
			_, _ = w.Write([]byte(`{"token":"t","expires":"2099-01-01T00:00:00Z"}`))
			return
		}
		if r.URL.Path != "/api/nginx/proxy-hosts" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		seen = append(seen, r.URL.Query().Get("expand"))
		if strings.Contains(r.URL.Query().Get("expand"), "access_list") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":500,"message":"Internal Error"}}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":1,"domain_names":["jellyfin.home.lab"],"enabled":true}]`))
	}))
	defer srv.Close()
	if status, answer := b.call(b.client, "POST", "/api/npm", map[string]string{"url": srv.URL, "email": "robin@example.com", "password": "x"}); status != 204 {
		t.Fatalf("linking: %d %v", status, answer)
	}
	list := func() (int, string) {
		res, err := b.client.Get(b.server.URL + "/api/npm/1/proxy-hosts")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	if status, raw := list(); status != 200 || !strings.Contains(raw, "jellyfin.home.lab") {
		t.Fatalf("listing: %d %s (asked %q)", status, raw, seen)
	}
	seen = nil
	if status, _ := list(); status != 200 || len(seen) != 1 || seen[0] != "certificate" {
		t.Fatalf("listing again asked %q", seen)
	}
}
