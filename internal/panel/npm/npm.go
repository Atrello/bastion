// Package npm talks to Nginx Proxy Manager's API: signing in as one of its users, then reading and
// changing its proxy hosts, redirects, streams, 404 hosts, access lists and certificates. NPM does the
// proxying; Bastion is another way to manage it.
package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Kinds are the NPM resources Bastion manages, as NPM's API names them.
var Kinds = map[string]string{
	"proxy-hosts":       "/api/nginx/proxy-hosts",
	"redirection-hosts": "/api/nginx/redirection-hosts",
	"streams":           "/api/nginx/streams",
	"dead-hosts":        "/api/nginx/dead-hosts",
	"access-lists":      "/api/nginx/access-lists",
	"certificates":      "/api/nginx/certificates",
}

// Client is one NPM instance, holding its sign-in token until it's near expiry.
type Client struct {
	URL, Email, Password string

	mu      sync.Mutex
	token   string
	expires time.Time
}

var httpClient = &http.Client{Timeout: 60 * time.Second} // requesting a certificate can take a while

// CheckURL makes sure an NPM address is a web address with nothing after the host.
func CheckURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("NPM's address is where its admin page is, like http://192.168.1.20:81")
	}
	return u.Scheme + "://" + u.Host, nil
}

// Error is something NPM refused, with its own message.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func (c *Client) signIn(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > 5*time.Minute {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"identity": c.Email, "secret": c.Password})
	var reply struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
	}
	if err := c.do(ctx, "", http.MethodPost, "/api/tokens", body, &reply); err != nil {
		return "", fmt.Errorf("signing in to NPM: %w", err)
	}
	c.token = reply.Token
	c.expires, _ = time.Parse(time.RFC3339, reply.Expires)
	if c.expires.IsZero() {
		c.expires = time.Now().Add(time.Hour)
	}
	return c.token, nil
}

// Call makes a signed-in request to NPM's API, decoding the answer into result (if it's not nil).
func (c *Client) Call(ctx context.Context, method, path string, body []byte, result any) error {
	token, err := c.signIn(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, token, method, path, body, result)
	var npmErr *Error
	if errors.As(err, &npmErr) && npmErr.Status == http.StatusUnauthorized {
		// The token's gone stale (NPM restarted, say): sign in again once.
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if token, err = c.signIn(ctx); err == nil {
			err = c.do(ctx, token, method, path, body, result)
		}
	}
	return err
}

func (c *Client) do(ctx context.Context, token, method, path string, body []byte, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("NPM can't be reached: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		message := res.Status
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			message = e.Error.Message
		}
		return &Error{Status: res.StatusCode, Message: message}
	}
	if result != nil && len(raw) > 0 {
		return json.Unmarshal(raw, result)
	}
	return nil
}
