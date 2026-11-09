// Package npm talks to Nginx Proxy Manager's API: signing in as one of its users, then reading and
// changing its proxy hosts, redirects, streams, 404 hosts, access lists and certificates. NPM does the
// proxying; Bastion is another way to manage it.
package npm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The NPMs Bastion understands.
const (
	FlavourNPM     = "npm"
	FlavourNPMplus = "npmplus"
)

// tokenCookie is where NPMplus puts its sign-in token.
const tokenCookie = "__Host-Http-token"

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
	// PinnedCert is the SHA-256 of the one certificate trusted for an NPM with a self-signed one (empty
	// for a certificate that's trusted the usual way, or for plain http). Only that certificate is accepted.
	PinnedCert string

	pinnedOnce   sync.Once
	pinnedClient *http.Client

	// Flavour is which NPM this is, found when signing in: FlavourNPM gives its token in its answer,
	// FlavourNPMplus (a fork with different fields) sets it as a cookie.
	Flavour string

	mu      sync.Mutex
	token   string
	expires time.Time
	schema  *schemaCache
}

const timeout = 60 * time.Second // requesting a certificate can take a while

var httpClient = &http.Client{Timeout: timeout}

// client is the HTTP client for this NPM: the usual one, or one that trusts only its pinned certificate.
func (c *Client) client() *http.Client {
	if c.PinnedCert == "" {
		return httpClient
	}
	c.pinnedOnce.Do(func() { c.pinnedClient = pinnedClient(c.PinnedCert) })
	return c.pinnedClient
}

func pinnedClient(pin string) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The usual checks are replaced by this one: the certificate must be exactly the one trusted.
			InsecureSkipVerify: true, //nolint:gosec // VerifyPeerCertificate checks the pinned certificate instead
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 || subtle.ConstantTimeCompare([]byte(Fingerprint(raw[0])), []byte(pin)) != 1 {
					return errors.New("NPM's certificate has changed since it was trusted")
				}
				return nil
			},
		},
	}}
}

// Fingerprint is a certificate's SHA-256, as hex pairs: "AB:CD:…".
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(pairs, ":")
}

// UntrustedCert is an NPM whose certificate isn't trusted the usual way (self-signed, say), with what's
// needed to ask whether to trust it.
type UntrustedCert struct {
	Fingerprint string    `json:"fingerprint"`
	Subject     string    `json:"subject"`
	Expires     time.Time `json:"expires"`
}

// ReadCert fetches the certificate an https:// NPM presents, without trusting or sending anything, so the
// person linking it can decide whether to trust it.
func ReadCert(ctx context.Context, address string) (*UntrustedCert, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" {
		return nil, errors.New("not https")
	}
	host := u.Host
	if u.Port() == "" {
		host += ":443"
	}
	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}} //nolint:gosec // only reads the certificate
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("no certificate")
	}
	subject := certs[0].Subject.CommonName
	if subject == "" {
		subject = certs[0].Subject.String()
	}
	return &UntrustedCert{Fingerprint: Fingerprint(certs[0].Raw), Subject: subject, Expires: certs[0].NotAfter}, nil
}

// IsUntrustedCert says whether an error is a certificate that isn't trusted the usual way.
func IsUntrustedCert(err error) bool {
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	return errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &hostname)
}

// CheckURL makes sure an NPM address is a web address with nothing after the host.
func CheckURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("NPM's address is where its admin page is, like http://192.168.1.20:81")
	}
	return u.Scheme + "://" + u.Host, nil
}

// unreachable is NPM not answering, said in words, keeping the cause for errors.As.
type unreachable struct {
	address string
	err     error
}

func (u *unreachable) Unwrap() error { return u.err }

func (u *unreachable) Error() string {
	var dnsErr *net.DNSError
	var netErr net.Error
	why := u.err.Error()
	switch {
	case IsUntrustedCert(u.err):
		why = "its certificate can't be checked. If it's self-signed, link it again and trust that certificate"
	case strings.Contains(why, "certificate has changed since it was trusted"):
		why = "its certificate has changed since it was trusted. If you changed it, unlink NPM and link it again"
	case errors.Is(u.err, syscall.ECONNREFUSED):
		why = "nothing answered there. Check it's running and the address and port are right"
	case errors.As(u.err, &dnsErr):
		why = "its name can't be looked up"
	case errors.As(u.err, &netErr) && netErr.Timeout():
		why = "it didn't answer in time"
	case strings.Contains(why, "server gave HTTP response to HTTPS client"):
		why = "it answers plain HTTP: use http:// in its address"
	case strings.Contains(why, "first record does not look like a TLS handshake"):
		why = "it answers plain HTTP: use http:// in its address"
	}
	return fmt.Sprintf("NPM can't be reached at %s: %s", u.address, why)
}

// Error is something NPM refused, with its own message.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Status == http.StatusForbidden || strings.Contains(strings.ToLower(e.Message), "permission") {
		return e.Message + ". The NPM user Bastion signs in as needs to be an administrator in NPM, or have Manage for " +
			"proxy hosts, redirection hosts, streams, 404 hosts, access lists and certificates (NPM's Users page)"
	}
	return e.Message
}

func (c *Client) signIn(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > 5*time.Minute {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"identity": c.Email, "secret": c.Password})
	var reply struct {
		Token        string `json:"token"`
		Expires      string `json:"expires"`
		RequiresTotp bool   `json:"requiresTotp"`
	}
	res, err := c.send(ctx, "", http.MethodPost, "/api/tokens", body, "application/json", &reply)
	if err != nil {
		return "", fmt.Errorf("signing in to NPM: %w", err)
	}
	switch {
	case reply.RequiresTotp:
		return "", errors.New("this NPM user has two-factor sign-in on, which Bastion can't answer: give Bastion its own NPM user without it")
	case reply.Token != "":
		c.Flavour, c.token = FlavourNPM, reply.Token
	default:
		// NPMplus sets the token as a cookie rather than giving it in its answer.
		for _, ck := range res.Cookies() {
			if ck.Name == tokenCookie {
				c.Flavour, c.token = FlavourNPMplus, ck.Value
			}
		}
		if c.token == "" {
			return "", errors.New("signing in to NPM: it didn't give a token")
		}
	}
	c.expires, _ = time.Parse(time.RFC3339, reply.Expires)
	if c.expires.IsZero() {
		c.expires = time.Now().Add(time.Hour)
	}
	return c.token, nil
}

// Call makes a signed-in request to NPM's API, decoding the answer into result (if it's not nil).
func (c *Client) Call(ctx context.Context, method, path string, body []byte, result any) error {
	return c.call(ctx, method, path, body, "application/json", result)
}

// Upload sends files to NPM as a form, the way it takes a custom certificate and its key: each file's
// form field name and its contents.
func (c *Client) Upload(ctx context.Context, path string, files map[string]string, result any) error {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for field, contents := range files {
		w, err := form.CreateFormFile(field, field+".pem")
		if err != nil {
			return err
		}
		_, _ = io.WriteString(w, contents)
	}
	if err := form.Close(); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPost, path, body.Bytes(), form.FormDataContentType(), result)
}

func (c *Client) call(ctx context.Context, method, path string, body []byte, contentType string, result any) error {
	token, err := c.signIn(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, token, method, path, body, contentType, result)
	var npmErr *Error
	if errors.As(err, &npmErr) && npmErr.Status == http.StatusUnauthorized {
		// The token's gone stale (NPM restarted, say): sign in again once.
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if token, err = c.signIn(ctx); err == nil {
			err = c.do(ctx, token, method, path, body, contentType, result)
		}
	}
	return err
}

func (c *Client) do(ctx context.Context, token, method, path string, body []byte, contentType string, result any) error {
	_, err := c.send(ctx, token, method, path, body, contentType, result)
	return err
}

// send makes a request, giving the response (its body already read into result) for its cookies.
func (c *Client) send(ctx context.Context, token, method, path string, body []byte, contentType string, result any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		if c.Flavour == FlavourNPMplus {
			req.AddCookie(&http.Cookie{Name: tokenCookie, Value: token})
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, &unreachable{address: c.URL, err: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
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
		return res, &Error{Status: res.StatusCode, Message: message}
	}
	if result != nil && len(raw) > 0 {
		return res, json.Unmarshal(raw, result)
	}
	return res, nil
}
