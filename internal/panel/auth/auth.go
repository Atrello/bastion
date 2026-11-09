package auth

import (
	"database/sql"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Auth holds what signing in needs: the database, the panel's address (passkeys are tied to it), and the
// short-lived state between the steps of signing in.
type Auth struct {
	db          *sql.DB
	origin      *url.URL
	sessionIdle time.Duration
	// Nil when the panel is reached by an IP address: passkeys need a name like bastion.home.lab.
	webauthn *webauthn.WebAuthn
	Limiter  *Limiter
	// Steps in progress: after the password and before the second factor, during first-run setup, and
	// passkey ceremonies. Each lasts a few minutes at most.
	Pending *Pending
}

func New(db *sql.DB, origin *url.URL, sessionIdle time.Duration) (*Auth, error) {
	var w *webauthn.WebAuthn
	if host := origin.Hostname(); net.ParseIP(host) == nil {
		var err error
		w, err = webauthn.New(&webauthn.Config{
			RPID:          host,
			RPDisplayName: "Bastion",
			RPOrigins:     []string{origin.Scheme + "://" + origin.Host},
		})
		if err != nil {
			return nil, err
		}
	}
	return &Auth{
		db: db, origin: origin, sessionIdle: sessionIdle, webauthn: w,
		Limiter: NewLimiter(), Pending: NewPending(),
	}, nil
}

// Limiter slows down guessing: after 5 failed attempts for an account, or 20 from one address, it waits
// 15 minutes, then longer after each further failure. A success clears the account's count. An empty
// account counts towards the address only (passkeys and setup links, where there's no account to name),
// so nobody can lock others out by failing on their behalf.
type Limiter struct {
	mu   sync.Mutex
	seen map[string]*attempts
}

type attempts struct {
	failures int
	until    time.Time
}

const (
	accountLimit = 5
	addressLimit = 20
	lockout      = 15 * time.Minute
)

func NewLimiter() *Limiter { return &Limiter{seen: map[string]*attempts{}} }

// Wait says how long the account or address must wait before trying again; zero means it can try now.
func (l *Limiter) Wait(account, address string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for key := range l.keys(account, address) {
		if a := l.seen[key]; a != nil {
			wait = max(wait, time.Until(a.until))
		}
	}
	return max(wait, 0)
}

func (l *Limiter) Failed(account, address string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, limit := range l.keys(account, address) {
		a := l.seen[key]
		if a == nil {
			a = &attempts{}
			l.seen[key] = a
		}
		a.failures++
		if a.failures >= limit {
			// 15 minutes, doubling with each failure past the limit, up to a day.
			a.until = time.Now().Add(min(lockout<<(a.failures-limit), 24*time.Hour))
		}
	}
}

// keys are the counts an attempt goes towards, with each one's limit.
func (l *Limiter) keys(account, address string) map[string]int {
	keys := map[string]int{"address:" + address: addressLimit}
	if account != "" {
		keys["account:"+account] = accountLimit
	}
	return keys
}

func (l *Limiter) Succeeded(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.seen, "account:"+account)
}

// ClientAddress is the address a request came from, without the port.
func ClientAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// Pending holds short-lived state by a random key kept in a cookie: what step of signing in someone's
// on. It's in memory: a restart of the panel means starting the sign-in again, which is fine. It holds
// at most maxPending items, so it can't be filled up to use the panel's memory.
type Pending struct {
	mu    sync.Mutex
	items map[string]pendingItem
}

type pendingItem struct {
	value   any
	expires time.Time
}

const maxPending = 10_000

var ErrBusy = errors.New("the panel is busy with too many sign-ins: try again in a few minutes")

func NewPending() *Pending { return &Pending{items: map[string]pendingItem{}} }

// Put keeps value for ttl, giving the key to find it by.
func (p *Pending) Put(value any, ttl time.Duration) (string, error) {
	key := RandomToken()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.items) >= maxPending/2 { // tidy away anything expired first
		for k, item := range p.items {
			if time.Now().After(item.expires) {
				delete(p.items, k)
			}
		}
	}
	if len(p.items) >= maxPending {
		return "", ErrBusy
	}
	p.items[hashToken(key)] = pendingItem{value: value, expires: time.Now().Add(ttl)}
	return key, nil
}

// Get finds what's kept under key, if it hasn't expired.
func (p *Pending) Get(key string) (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.items[hashToken(key)]
	if !ok || time.Now().After(item.expires) {
		return nil, false
	}
	return item.value, true
}

func (p *Pending) Delete(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, hashToken(key))
}
