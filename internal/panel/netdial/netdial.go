// Package netdial makes the panel's outgoing connections (alerts, NPM, S3, eggs, email), looking a name
// up the usual way first and, only if that fails, asking public DNS servers instead. Docker's DNS inside
// a container passes lookups to the host's resolver, which some hosts (systemd-resolved, Pi-hole,
// AdGuard) don't answer for containers: without this, "lookup ntfy.sh: server misbehaving". A name the
// usual way finds (a local name only your own DNS knows) is never sent to the public ones.
package netdial

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	mu       sync.RWMutex
	fallback = []string{"1.1.1.1", "9.9.9.9"}
)

// Use sets the DNS servers asked when a lookup fails ("1.1.1.1,9.9.9.9"; "off" for none), and makes Go's
// default HTTP transport dial through DialContext.
func Use(servers string) {
	mu.Lock()
	switch s := strings.TrimSpace(servers); s {
	case "":
	case "off":
		fallback = nil
	default:
		fallback = nil
		for _, f := range strings.Split(s, ",") {
			if ip := net.ParseIP(strings.TrimSpace(f)); ip != nil {
				fallback = append(fallback, ip.String())
			}
		}
	}
	mu.Unlock()
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.DialContext = DialContext
	}
}

var dialer = &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}

// DialContext connects to addr ("host:port"), trying each address the host has.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		return dialer.DialContext(ctx, network, addr)
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func lookup(ctx context.Context, host string) ([]string, error) {
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err == nil || ctx.Err() != nil {
		return ips, err
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound && !dnsErr.IsTemporary && strings.Count(host, ".") == 0 {
		return nil, err // a bare name that doesn't exist: the public servers won't know it either
	}
	mu.RLock()
	servers := append([]string(nil), fallback...)
	mu.RUnlock()
	for _, server := range servers {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
		if found, ferr := r.LookupHost(ctx, host); ferr == nil && len(found) > 0 {
			return found, nil
		}
	}
	return nil, err
}
