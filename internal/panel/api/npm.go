package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Atrello/bastion/internal/panel/netdial"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/npm"
)

// Nginx Proxy Manager: instances the panel signs in to, and their proxy hosts, redirects, streams, 404
// hosts, access lists and certificates passed through as NPM has them (so every field NPM has, from custom
// locations to advanced config, comes through). The panel also checks each proxy host every 5 minutes.

type npmState struct {
	mu      sync.Mutex
	clients map[int64]*npm.Client
	// Each proxy host's last check, by instance and host ID.
	health map[int64]map[int64]hostHealth
	// The expand each NPM accepted for each kind of list, by instance and kind (see npmList).
	expands map[int64]map[string]string
}

type hostHealth struct {
	Status    int    `json:"status"` // the HTTP status, or 0 when it couldn't be reached
	Millis    int64  `json:"ms"`
	Error     string `json:"error,omitempty"`
	CheckedAt string `json:"checkedAt"`
	domain    string
}

func (api *API) npmRoutes() {
	api.npm = &npmState{clients: map[int64]*npm.Client{}, health: map[int64]map[int64]hostHealth{}, expands: map[int64]map[string]string{}}
	m := api.mux
	m.Handle("GET /api/npm", api.owner(api.npmInstances))
	m.Handle("POST /api/npm", api.owner(api.addNPM))
	m.Handle("GET /api/npm/summary", api.owner(api.npmSummary))
	m.Handle("DELETE /api/npm/{id}", api.owner(api.deleteNPM))
	m.Handle("GET /api/npm/{id}/{kind}", api.owner(api.npmList))
	m.Handle("POST /api/npm/{id}/{kind}", api.owner(api.npmChange))
	m.Handle("PUT /api/npm/{id}/{kind}/{item}", api.owner(api.npmChange))
	m.Handle("DELETE /api/npm/{id}/{kind}/{item}", api.owner(api.npmChange))
	m.Handle("POST /api/npm/{id}/{kind}/{item}/{action}", api.owner(api.npmAction))
	m.Handle("POST /api/npm/{id}/certificates/{item}/upload", api.owner(api.npmUpload))
}

func (api *API) npmClient(ctx context.Context, id int64) (*npm.Client, error) {
	api.npm.mu.Lock()
	defer api.npm.mu.Unlock()
	if c := api.npm.clients[id]; c != nil {
		return c, nil
	}
	c := &npm.Client{}
	var flavour string
	err := api.db.QueryRowContext(ctx, `SELECT url, email, password, cert_sha256, flavour FROM npm_instances WHERE id = ?`, id).Scan(&c.URL, &c.Email, &c.Password, &c.PinnedCert, &flavour)
	if err != nil {
		return nil, errors.New("no such NPM")
	}
	c.SetFlavour(flavour)
	api.npm.clients[id] = c
	return c, nil
}

func (api *API) npmInstances(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT id, name, url, email, flavour FROM npm_instances ORDER BY id`)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	type instance struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		URL   string `json:"url"`
		Email string `json:"email"`
		// npm or npmplus: their forms differ.
		Flavour string `json:"flavour"`
	}
	list := []instance{}
	for rows.Next() {
		var i instance
		if rows.Scan(&i.ID, &i.Name, &i.URL, &i.Email, &i.Flavour) == nil {
			list = append(list, i)
		}
	}
	writeJSON(w, list)
}

// addNPM links an NPM instance, signing in first to check the details work. An https:// NPM whose
// certificate isn't trusted the usual way (self-signed) answers 409 with the certificate's fingerprint; sent
// again with trustCertificate set to that fingerprint, that one certificate is trusted from then on.
func (api *API) addNPM(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name, URL, Email, Password string
		TrustCertificate           string `json:"trustCertificate"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	address, err := npm.CheckURL(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pin := strings.ToUpper(strings.TrimSpace(body.TrustCertificate))
	c := &npm.Client{URL: address, Email: strings.TrimSpace(body.Email), Password: body.Password, PinnedCert: pin}
	var probe []json.RawMessage
	if err := c.Call(r.Context(), http.MethodGet, npm.Kinds["proxy-hosts"], nil, &probe); err != nil {
		if pin == "" && npm.IsUntrustedCert(err) {
			if cert, cerr := npm.ReadCert(r.Context(), address); cerr == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":       "NPM's certificate isn't one Bastion can check (it's probably self-signed)",
					"certificate": cert,
				})
				return
			}
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 60 {
		name = "Nginx Proxy Manager"
	}
	if _, err := api.db.ExecContext(r.Context(), `INSERT INTO npm_instances (name, url, email, password, cert_sha256, flavour) VALUES (?, ?, ?, ?, ?, ?)`,
		name, address, c.Email, c.Password, pin, c.Flavour()); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("NPM linked", "user", u.Username, "url", address, "flavour", c.Flavour())
	go api.checkNPM(context.WithoutCancel(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteNPM(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM npm_instances WHERE id = ?`, id); err != nil {
		api.fail(w, r, err)
		return
	}
	api.npm.mu.Lock()
	delete(api.npm.clients, id)
	delete(api.npm.health, id)
	api.npm.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// npmTarget reads the NPM instance, the kind of resource and (if given) the item from the path.
func (api *API) npmTarget(w http.ResponseWriter, r *http.Request) (*npm.Client, int64, string, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	kind := r.PathValue("kind")
	path, known := npm.Kinds[kind]
	if !known {
		writeError(w, http.StatusNotFound, "no such kind of NPM item")
		return nil, 0, "", false
	}
	if item := r.PathValue("item"); item != "" {
		n, err := strconv.ParseInt(item, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusNotFound, "no such item")
			return nil, 0, "", false
		}
		path += "/" + item
	}
	c, err := api.npmClient(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return nil, 0, "", false
	}
	return c, id, path, true
}

func (api *API) npmFail(w http.ResponseWriter, err error) {
	var npmErr *npm.Error
	if errors.As(err, &npmErr) {
		writeError(w, http.StatusBadRequest, "NPM: "+npmErr.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (api *API) npmList(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	c, id, path, ok := api.npmTarget(w, r)
	if !ok {
		return
	}
	items, err := api.npmListExpanded(r.Context(), c, id, r.PathValue("kind"), path)
	if err != nil {
		api.npmFail(w, err)
		return
	}
	for _, item := range items {
		scrub(item)
	}
	if r.PathValue("kind") == "proxy-hosts" {
		api.npm.mu.Lock()
		for _, item := range items {
			if hostID, ok := item["id"].(float64); ok {
				if h, found := api.npm.health[id][int64(hostID)]; found {
					item["bastion_health"] = h
				}
			}
		}
		api.npm.mu.Unlock()
	}
	writeJSON(w, items)
}

// What each list asks NPM to include with its items (a host's certificate and access lists, say), most
// wanted first. The names differ between NPM, NPMplus and their versions (NPMplus has "access_lists", as a
// host can have several), and one NPM doesn't know fails the whole list ("Internal Error"), so the next is
// tried, down to none, and the one that worked is remembered.
var npmExpands = map[string][]string{
	"proxy-hosts":       {"certificate,access_list", "certificate,access_lists", "certificate", ""},
	"redirection-hosts": {"certificate", ""},
	"dead-hosts":        {"certificate", ""},
	"access-lists":      {"items,clients", "items", ""},
	"certificates":      {""},
}

func (api *API) npmListExpanded(ctx context.Context, c *npm.Client, id int64, kind, path string) ([]map[string]any, error) {
	tries := npmExpands[kind]
	if kind == "proxy-hosts" && c.Flavour() == npm.FlavourNPMplus {
		tries = []string{"certificate,access_lists", "certificate,access_list", "certificate", ""}
	}
	api.npm.mu.Lock()
	if known, ok := api.npm.expands[id][kind]; ok {
		tries = append([]string{known}, tries...)
	}
	api.npm.mu.Unlock()

	var lastErr error
	for i, expand := range tries {
		if i > 0 && expand == tries[0] {
			continue
		}
		p := path
		if expand != "" {
			p += "?expand=" + expand
		}
		var items []map[string]any
		err := c.Call(ctx, http.MethodGet, p, nil, &items)
		var npmErr *npm.Error
		if errors.As(err, &npmErr) && (npmErr.Status == http.StatusInternalServerError || npmErr.Status == http.StatusBadRequest) {
			lastErr = err
			continue // this NPM doesn't know that expand: try the next
		}
		if err != nil {
			return nil, err
		}
		api.npm.mu.Lock()
		if api.npm.expands[id] == nil {
			api.npm.expands[id] = map[string]string{}
		}
		api.npm.expands[id][kind] = expand
		api.npm.mu.Unlock()
		return items, nil
	}
	return nil, lastErr
}

// npmChange passes a create, update or delete through to NPM, with the item as NPM has it.
func (api *API) npmChange(w http.ResponseWriter, r *http.Request, u *auth.User) {
	c, _, path, ok := api.npmTarget(w, r)
	if !ok {
		return
	}
	var body []byte
	if r.Method != http.MethodDelete {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
		var object map[string]any
		if err != nil || json.Unmarshal(raw, &object) != nil {
			writeError(w, http.StatusBadRequest, "send the item as a JSON object")
			return
		}
		// Only the fields this NPM (or NPMplus, of whichever version) takes for it.
		c.Prune(r.Context(), r.Method, path, object)
		body, _ = json.Marshal(object)
	}
	var result any
	if err := c.Call(r.Context(), r.Method, path, body, &result); err != nil {
		api.npmFail(w, err)
		return
	}
	scrub(result)
	api.log.Info("NPM change", "user", u.Username, "method", r.Method, "path", path)
	if r.PathValue("kind") == "proxy-hosts" {
		go api.checkNPM(context.WithoutCancel(r.Context()))
	}
	writeJSON(w, result)
}

// npmAction enables or disables a host, or renews a certificate.
func (api *API) npmAction(w http.ResponseWriter, r *http.Request, u *auth.User) {
	c, _, path, ok := api.npmTarget(w, r)
	if !ok {
		return
	}
	action, kind := r.PathValue("action"), r.PathValue("kind")
	allowed := (kind == "certificates" && action == "renew") ||
		(kind != "certificates" && kind != "access-lists" && (action == "enable" || action == "disable"))
	if !allowed {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	var result any
	if err := c.Call(r.Context(), http.MethodPost, path+"/"+action, nil, &result); err != nil {
		api.npmFail(w, err)
		return
	}
	scrub(result)
	api.log.Info("NPM action", "user", u.Username, "path", path, "action", action)
	if kind == "proxy-hosts" {
		go api.checkNPM(context.WithoutCancel(r.Context()))
	}
	writeJSON(w, result)
}

// secretFields are what NPM sends back that the browser never needs: a custom certificate's private key,
// a DNS provider's credentials, and access list passwords.
var secretFields = []string{"certificate_key", "dns_provider_credentials", "npmplus_dns_provider_credentials", "password"}

// scrub removes secretFields from an answer from NPM, wherever they are in it.
func scrub(v any) {
	switch v := v.(type) {
	case map[string]any:
		for _, field := range secretFields {
			delete(v, field)
		}
		for _, child := range v {
			scrub(child)
		}
	case []any:
		for _, child := range v {
			scrub(child)
		}
	}
}

// npmSummary is the overview's line on the proxy: how many hosts answer, and anything wrong.
func (api *API) npmSummary(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	type issue struct {
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	summary := struct {
		Hosts    int     `json:"hosts"`
		Up       int     `json:"up"`
		Problems []issue `json:"problems"`
	}{Problems: []issue{}}
	api.npm.mu.Lock()
	for _, hosts := range api.npm.health {
		for _, h := range hosts {
			summary.Hosts++
			if h.Status != 0 && h.Status < 500 {
				summary.Up++
			}
		}
	}
	api.npm.mu.Unlock()
	for _, p := range api.npmProblems(r.Context()) {
		summary.Problems = append(summary.Problems, issue{p.kind, p.title, p.detail})
	}
	writeJSON(w, summary)
}

// npmUpload gives a custom certificate (made with provider "other") its certificate, key and any
// intermediate certificate, as PEM text.
func (api *API) npmUpload(w http.ResponseWriter, r *http.Request, u *auth.User) {
	r.SetPathValue("kind", "certificates")
	c, _, path, ok := api.npmTarget(w, r)
	if !ok {
		return
	}
	var body struct{ Certificate, CertificateKey, Intermediate string }
	if !readJSON(w, r, &body) {
		return
	}
	if !strings.Contains(body.Certificate, "-----BEGIN CERTIFICATE-----") || !strings.Contains(body.CertificateKey, "PRIVATE KEY-----") {
		writeError(w, http.StatusBadRequest, "paste the certificate and its private key, each starting -----BEGIN")
		return
	}
	files := map[string]string{"certificate": body.Certificate, "certificate_key": body.CertificateKey}
	if strings.TrimSpace(body.Intermediate) != "" {
		if c.Flavour() == npm.FlavourNPMplus {
			// NPMplus takes the chain in the certificate's own file.
			files["certificate"] = strings.TrimSpace(body.Certificate) + "\n" + strings.TrimSpace(body.Intermediate) + "\n"
		} else {
			files["intermediate_certificate"] = body.Intermediate
		}
	}
	// NPM answers with the files, key included: that isn't sent back.
	if err := c.Upload(r.Context(), path+"/upload", files, nil); err != nil {
		api.npmFail(w, err)
		return
	}
	api.log.Info("NPM certificate uploaded", "user", u.Username, "path", path)
	w.WriteHeader(http.StatusNoContent)
}

// The health check's client: it doesn't follow redirects (an HTTP→HTTPS redirect is a healthy answer)
// and accepts any certificate, as it's checking the site answers, not vouching for it.
var healthClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{DialContext: netdial.DialContext, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // see above
}

// watchNPM checks every enabled proxy host on every NPM every 5 minutes.
func (api *API) watchNPM(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		api.checkNPM(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type proxyHost struct {
	ID          int64    `json:"id"`
	DomainNames []string `json:"domain_names"`
	Enabled     any      `json:"enabled"`
	SSLForced   any      `json:"ssl_forced"`
	Certificate int64    `json:"certificate_id"`
}

func truthy(v any) bool { return v == true || v == float64(1) }

func (api *API) checkNPM(ctx context.Context) {
	rows, err := api.db.QueryContext(ctx, `SELECT id FROM npm_instances`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		c, err := api.npmClient(ctx, id)
		if err != nil {
			continue
		}
		var hosts []proxyHost
		if c.Call(ctx, http.MethodGet, npm.Kinds["proxy-hosts"], nil, &hosts) != nil {
			continue
		}
		results := map[int64]hostHealth{}
		for _, h := range hosts {
			if !truthy(h.Enabled) || len(h.DomainNames) == 0 || strings.HasPrefix(h.DomainNames[0], "*") {
				continue
			}
			scheme := "http"
			if h.Certificate > 0 {
				scheme = "https"
			}
			start := time.Now()
			hh := hostHealth{CheckedAt: start.UTC().Format(time.RFC3339), domain: h.DomainNames[0]}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+h.DomainNames[0]+"/", nil)
			if res, err := healthClient.Do(req); err != nil {
				hh.Error = unreachable(err)
			} else {
				res.Body.Close()
				hh.Status = res.StatusCode
			}
			hh.Millis = time.Since(start).Milliseconds()
			results[h.ID] = hh
		}
		api.npm.mu.Lock()
		api.npm.health[id] = results
		api.npm.mu.Unlock()
	}
}

// unreachable says why a host didn't answer, in words.
func unreachable(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "Bastion can't look up its name, so it can't check it."
	case errors.As(err, &netErr) && netErr.Timeout():
		return "It didn't answer within 10 seconds."
	default:
		return "It didn't answer: " + err.Error()
	}
}

// npmProblems is what's wrong with the NPM instances now, for alerts: a proxy host that doesn't answer
// (or answers with a server error), and a certificate expiring within 14 days.
func (api *API) npmProblems(ctx context.Context) []problem {
	var found []problem
	api.npm.mu.Lock()
	for id, hosts := range api.npm.health {
		for hostID, h := range hosts {
			if h.Status == 0 || h.Status >= 500 {
				detail := h.Error
				if h.Status != 0 {
					detail = fmt.Sprintf("It answered %d.", h.Status)
				}
				found = append(found, problem{fmt.Sprintf("host:%d:%d", id, hostID), "host", h.domain + " isn't answering", detail})
			}
		}
	}
	ids := make([]int64, 0, len(api.npm.health))
	for id := range api.npm.health {
		ids = append(ids, id)
	}
	api.npm.mu.Unlock()
	for _, id := range ids {
		c, err := api.npmClient(ctx, id)
		if err != nil {
			continue
		}
		var certs []struct {
			ID          int64    `json:"id"`
			NiceName    string   `json:"nice_name"`
			DomainNames []string `json:"domain_names"`
			ExpiresOn   string   `json:"expires_on"`
			CreatedOn   string   `json:"created_on"`
		}
		if c.Call(ctx, http.MethodGet, npm.Kinds["certificates"], nil, &certs) != nil {
			continue
		}
		for _, cert := range certs {
			expires, err := time.Parse("2006-01-02 15:04:05", cert.ExpiresOn)
			if err != nil {
				expires, err = time.Parse(time.RFC3339, cert.ExpiresOn)
			}
			// A custom certificate waiting for its files has its creation time as its expiry.
			if err == nil && cert.ExpiresOn != cert.CreatedOn && time.Until(expires) < 14*24*time.Hour {
				name := cert.NiceName
				if name == "" && len(cert.DomainNames) > 0 {
					name = cert.DomainNames[0]
				}
				found = append(found, problem{fmt.Sprintf("certificate:%d:%d", id, cert.ID), "certificate", "Certificate for " + name + " expires soon",
					"It expires on " + expires.Format("2 Jan 2006") + ". NPM renews Let's Encrypt certificates by itself; if it hasn't, renew it from Certificates."})
			}
		}
	}
	return found
}
