package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Atrello/bastion/internal/panel/config"
	"github.com/Atrello/bastion/internal/panel/store"
	"github.com/pquerna/otp/totp"
)

// A browser talking to a fresh panel: it keeps cookies and sends the panel's Origin, like the web app.
type browser struct {
	t      *testing.T
	api    *API
	server *httptest.Server
	client *http.Client
}

func newPanel(t *testing.T) *browser {
	t.Helper()
	db, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b := &browser{t: t}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { b.api.ServeHTTP(w, r) }))
	t.Cleanup(b.server.Close)
	// Passkeys need a name, not an IP address: the panel is "localhost" to its browsers here.
	b.server.URL = strings.Replace(b.server.URL, "127.0.0.1", "localhost", 1)
	public, _ := url.Parse(b.server.URL)
	cfg := &config.Config{PublicURL: public, SessionIdle: time.Hour, AgentBinaries: t.TempDir(), Version: "0.4.0"}
	b.api, err = New(context.Background(), cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	b.client = b.newClient()
	return b
}

func (b *browser) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// call sends a request with a JSON body (or none), giving the status and the decoded answer.
func (b *browser) call(client *http.Client, method, path string, body any, headers ...string) (int, map[string]any) {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.server.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", b.server.URL)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// setUp makes the owner's account with an authenticator app, giving its secret and recovery codes.
func (b *browser) setUp() (secret string, codes []any) {
	b.t.Helper()
	token, _ := b.api.auth.NewSetupToken(context.Background())
	if status, _ := b.call(b.client, "POST", "/api/setup", map[string]string{"token": token, "username": "robin", "password": "correct horse battery"}); status != 204 {
		b.t.Fatalf("setup: %d", status)
	}
	_, begun := b.call(b.client, "POST", "/api/auth/authenticator/begin", map[string]string{"password": ""})
	secret = begun["secret"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	status, done := b.call(b.client, "POST", "/api/auth/authenticator/finish", map[string]string{"code": code})
	if status != 200 {
		b.t.Fatalf("authenticator finish: %d %v", status, done)
	}
	return secret, done["recoveryCodes"].([]any)
}

func TestSetupMakesTheOwnerAndSignsIn(t *testing.T) {
	b := newPanel(t)
	_, state := b.call(b.client, "GET", "/api/auth/state", nil)
	if state["setupNeeded"] != true {
		t.Fatal("a new panel should need setting up")
	}
	_, codes := b.setUp()
	if len(codes) != 10 {
		t.Fatalf("want 10 recovery codes, got %d", len(codes))
	}
	_, state = b.call(b.client, "GET", "/api/auth/state", nil)
	if state["signedIn"] != true || state["setupNeeded"] != false {
		t.Fatalf("after setup: %v", state)
	}
	// The setup link has been used up.
	token, _ := b.api.auth.NewSetupToken(context.Background())
	if status, _ := b.call(b.newClient(), "POST", "/api/setup", map[string]string{"token": token, "username": "mallory", "password": "correct horse battery"}); status != 400 {
		t.Fatalf("a second setup was allowed: %d", status)
	}
}

func TestSetupNeedsTheLinkAndASecondFactor(t *testing.T) {
	b := newPanel(t)
	if status, _ := b.call(b.client, "POST", "/api/setup", map[string]string{"token": "guess", "username": "robin", "password": "correct horse battery"}); status != 400 {
		t.Fatalf("a wrong setup link was accepted: %d", status)
	}
	token, _ := b.api.auth.NewSetupToken(context.Background())
	if status, _ := b.call(b.client, "POST", "/api/setup", map[string]string{"token": token, "username": "robin", "password": "short"}); status != 400 {
		t.Fatalf("a short password was accepted: %d", status)
	}
	b.call(b.client, "POST", "/api/setup", map[string]string{"token": token, "username": "robin", "password": "correct horse battery"})
	// Without a second factor, nothing is saved and nobody is signed in.
	if has, _ := b.api.auth.HasUsers(context.Background()); has {
		t.Fatal("an account was saved before its second factor")
	}
	if status, _ := b.call(b.client, "GET", "/api/account", nil); status != 401 {
		t.Fatalf("signed in without a second factor: %d", status)
	}
}

func TestSignInNeedsTheSecondFactor(t *testing.T) {
	b := newPanel(t)
	secret, codes := b.setUp()
	other := b.newClient()

	status, methods := b.call(other, "POST", "/api/auth/signin", map[string]string{"username": "Robin", "password": "correct horse battery"})
	if status != 200 || methods["code"] != true {
		t.Fatalf("password step: %d %v", status, methods)
	}
	if status, _ := b.call(other, "GET", "/api/account", nil); status != 401 {
		t.Fatal("signed in with a password alone")
	}
	if status, _ := b.call(other, "POST", "/api/auth/signin/code", map[string]string{"code": "000000"}); status != 401 {
		t.Fatal("a wrong code was accepted")
	}
	// The code from setup has been used, so use the next one (accepted, for clocks a little ahead).
	code, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if status, _ := b.call(other, "POST", "/api/auth/signin/code", map[string]string{"code": code}); status != 204 {
		t.Fatalf("the right code was refused: %d", status)
	}
	if status, _ := b.call(other, "GET", "/api/account", nil); status != 200 {
		t.Fatal("not signed in after the second factor")
	}

	// The same code can't be used twice, even by someone who saw it.
	third := b.newClient()
	b.call(third, "POST", "/api/auth/signin", map[string]string{"username": "robin", "password": "correct horse battery"})
	if status, _ := b.call(third, "POST", "/api/auth/signin/code", map[string]string{"code": code}); status != 401 {
		t.Fatal("a code was accepted twice")
	}
	// A recovery code works once.
	recovery := codes[0].(string)
	if status, _ := b.call(third, "POST", "/api/auth/signin/recovery", map[string]string{"code": recovery}); status != 204 {
		t.Fatal("a recovery code was refused")
	}
	fourth := b.newClient()
	b.call(fourth, "POST", "/api/auth/signin", map[string]string{"username": "robin", "password": "correct horse battery"})
	if status, _ := b.call(fourth, "POST", "/api/auth/signin/recovery", map[string]string{"code": recovery}); status != 401 {
		t.Fatal("a recovery code was accepted twice")
	}
}

func TestWrongPasswordsAreLimited(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	for range 5 {
		if status, answer := b.call(b.newClient(), "POST", "/api/auth/signin", map[string]string{"username": "robin", "password": "not it at all"}); status != 401 {
			t.Fatalf("a wrong password: %d %v", status, answer)
		}
	}
	// Now even the right password waits.
	if status, _ := b.call(b.newClient(), "POST", "/api/auth/signin", map[string]string{"username": "robin", "password": "correct horse battery"}); status != 429 {
		t.Fatalf("not limited after 5 wrong passwords: %d", status)
	}
	// A name that doesn't exist answers the same as a wrong password.
	if status, answer := b.call(b.newClient(), "POST", "/api/auth/signin", map[string]string{"username": "nobody", "password": "not it at all"}); status != 401 || answer["error"] != "that username or password isn't right" {
		t.Fatalf("an unknown name answered differently: %d %v", status, answer)
	}
}

func TestChangesMustComeFromThePanel(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	// Another site's page posting to the panel, even with the session cookie.
	status, _ := b.call(b.client, "POST", "/api/account/signout-everywhere", nil, "Origin", "https://evil.example")
	if status != 403 {
		t.Fatalf("a cross-site change was accepted: %d", status)
	}
	status, _ = b.call(b.client, "POST", "/api/account/signout-everywhere", nil, "Sec-Fetch-Site", "cross-site")
	if status != 403 {
		t.Fatalf("a cross-site change was accepted: %d", status)
	}
}

func TestAddingASecondFactorAsksForThePassword(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	if status, _ := b.call(b.client, "POST", "/api/auth/authenticator/begin", map[string]string{"password": "wrong password!"}); status != 400 {
		t.Fatalf("an authenticator could be replaced without the password: %d", status)
	}
	if status, _ := b.call(b.client, "POST", "/api/auth/passkey/register/begin", map[string]string{"name": "Phone", "password": "wrong password!"}); status != 400 {
		t.Fatalf("a passkey could be added without the password: %d", status)
	}
	if status, _ := b.call(b.client, "POST", "/api/auth/authenticator/begin", map[string]string{"password": "correct horse battery"}); status != 200 {
		t.Fatalf("the right password was refused: %d", status)
	}
}

func TestSignOut(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	b.call(b.client, "POST", "/api/auth/signout", nil)
	if status, _ := b.call(b.client, "GET", "/api/account", nil); status != 401 {
		t.Fatal("still signed in after signing out")
	}
}
