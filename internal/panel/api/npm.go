package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
}

type hostHealth struct {
	Status    int    `json:"status"` // the HTTP status, or 0 when it couldn't be reached
	Millis    int64  `json:"ms"`
	Error     string `json:"error,omitempty"`
	CheckedAt string `json:"checkedAt"`
	domain    string
}

func (api *API) npmRoutes() {
	api.npm = &npmState{clients: map[int64]*npm.Client{}, health: map[int64]map[int64]hostHealth{}}
	m := api.mux
	m.Handle("GET /api/npm", api.signedIn(api.npmInstances))
	m.Handle("POST /api/npm", api.signedIn(api.addNPM))
	m.Handle("DELETE /api/npm/{id}", api.signedIn(api.deleteNPM))
	m.Handle("GET /api/npm/{id}/{kind}", api.signedIn(api.npmList))
	m.Handle("POST /api/npm/{id}/{kind}", api.signedIn(api.npmChange))
	m.Handle("PUT /api/npm/{id}/{kind}/{item}", api.signedIn(api.npmChange))
	m.Handle("DELETE /api/npm/{id}/{kind}/{item}", api.signedIn(api.npmChange))
	m.Handle("POST /api/npm/{id}/{kind}/{item}/{action}", api.signedIn(api.npmAction))
}

func (api *API) npmClient(ctx context.Context, id int64) (*npm.Client, error) {
	api.npm.mu.Lock()
	defer api.npm.mu.Unlock()
	if c := api.npm.clients[id]; c != nil {
		return c, nil
	}
	c := &npm.Client{}
	err := api.db.QueryRowContext(ctx, `SELECT url, email, password FROM npm_instances WHERE id = ?`, id).Scan(&c.URL, &c.Email, &c.Password)
	if err != nil {
		return nil, errors.New("no such NPM")
	}
	api.npm.clients[id] = c
	return c, nil
}

func (api *API) npmInstances(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT id, name, url, email FROM npm_instances ORDER BY id`)
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
	}
	list := []instance{}
	for rows.Next() {
		var i instance
		if rows.Scan(&i.ID, &i.Name, &i.URL, &i.Email) == nil {
			list = append(list, i)
		}
	}
	writeJSON(w, list)
}

// addNPM links an NPM instance, signing in first to check the details work.
func (api *API) addNPM(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct{ Name, URL, Email, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	address, err := npm.CheckURL(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := &npm.Client{URL: address, Email: strings.TrimSpace(body.Email), Password: body.Password}
	var probe []json.RawMessage
	if err := c.Call(r.Context(), http.MethodGet, npm.Kinds["proxy-hosts"], nil, &probe); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 60 {
		name = "Nginx Proxy Manager"
	}
	if _, err := api.db.ExecContext(r.Context(), `INSERT INTO npm_instances (name, url, email, password) VALUES (?, ?, ?, ?)`,
		name, address, c.Email, c.Password); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("NPM linked", "user", u.Username, "url", address)
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
		writeError(w, http.StatusBadRequest, "NPM: "+npmErr.Message)
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (api *API) npmList(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	c, id, path, ok := api.npmTarget(w, r)
	if !ok {
		return
	}
	expand := map[string]string{
		"proxy-hosts": "certificate,access_list", "redirection-hosts": "certificate", "dead-hosts": "certificate",
		"access-lists": "items,clients", "certificates": "",
	}[r.PathValue("kind")]
	if expand != "" {
		path += "?expand=" + expand
	}
	var items []map[string]any
	if err := c.Call(r.Context(), http.MethodGet, path, nil, &items); err != nil {
		api.npmFail(w, err)
		return
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
		body = raw
	}
	var result any
	if err := c.Call(r.Context(), r.Method, path, body, &result); err != nil {
		api.npmFail(w, err)
		return
	}
	api.log.Info("NPM change", "user", u.Username, "method", r.Method, "path", path)
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
	api.log.Info("NPM action", "user", u.Username, "path", path, "action", action)
	writeJSON(w, result)
}

// The health check's client: it doesn't follow redirects (an HTTP→HTTPS redirect is a healthy answer)
// and accepts any certificate, as it's checking the site answers, not vouching for it.
var healthClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // see above
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
				hh.Error = err.Error()
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
		}
		if c.Call(ctx, http.MethodGet, npm.Kinds["certificates"], nil, &certs) != nil {
			continue
		}
		for _, cert := range certs {
			expires, err := time.Parse("2006-01-02 15:04:05", cert.ExpiresOn)
			if err != nil {
				expires, err = time.Parse(time.RFC3339, cert.ExpiresOn)
			}
			if err == nil && time.Until(expires) < 14*24*time.Hour {
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
