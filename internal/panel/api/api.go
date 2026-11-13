// Package api is the panel's HTTP API: what the web app (and, later, scripts) ask the panel to do.
// docs/api.md lists every endpoint.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"database/sql"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/config"
	"github.com/Atrello/bastion/internal/panel/store"
)

type API struct {
	cfg   *config.Config
	db    *sql.DB
	auth  *auth.Auth
	hub   *agents.Hub
	npm   *npmState
	games *gameState
	jobs  *jobList
	// Games' states as last seen, for notifications of changes.
	gameEvents *gameStates
	log        *slog.Logger
	mux        *http.ServeMux
}

// New builds the API and, on a panel nobody has set up yet, prints the one-time setup link to the log.
func New(ctx context.Context, cfg *config.Config, db *store.Store, log *slog.Logger) (*API, error) {
	a, err := auth.New(db.DB, cfg.PublicURL, cfg.SessionIdle)
	if err != nil {
		return nil, err
	}
	// The API's own log lines that name a user are the activity log too (activity.go).
	acts := slog.New(&activityHandler{Handler: log.Handler(), db: db.DB})
	api := &API{cfg: cfg, db: db.DB, auth: a, hub: agents.NewHub(db.DB, log), log: acts, mux: http.NewServeMux()}
	api.routes()

	if has, err := a.HasUsers(ctx); err != nil {
		return nil, err
	} else if !has {
		token, err := a.NewSetupToken(ctx)
		if err != nil {
			return nil, err
		}
		log.Warn("Bastion isn't set up yet. Open this link to make the owner's account: " + cfg.PublicURL.String() + "/setup#" + token)
	}
	if !a.PasskeysAvailable() {
		log.Warn("Passkeys are off: they need the panel to be reached by a name (BASTION_PUBLIC_URL), not an IP address")
	}
	go api.pruneSessions(ctx)
	go api.watchGit(ctx)
	go api.watchAlerts(ctx)
	go api.watchNPM(ctx)
	go api.runSchedules(ctx)
	return api, nil
}

func (api *API) routes() {
	api.authRoutes()
	api.serverRoutes()
	api.streamRoutes()
	api.gitRoutes()
	api.alertRoutes()
	api.npmRoutes()
	api.eggRoutes()
	api.gameRoutes()
	api.jobRoutes()
	api.backupStoreRoutes()
	api.notificationRoutes()
	api.activityRoutes()
	api.healthRoutes()
}

func (api *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Agents aren't browsers: they have no cookies and send no Origin, and sign in with their own secret.
	isAgent := strings.HasPrefix(r.URL.Path, "/api/agent/")
	if !isAgent && r.Method != http.MethodGet && r.Method != http.MethodHead && !api.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "requests that change things must come from the panel itself")
		return
	}
	api.mux.ServeHTTP(w, r)
}

// sameOrigin guards against cross-site request forgery: a change is only accepted from the panel's own
// pages. Browsers send Origin with every POST, PUT and DELETE, and Sec-Fetch-Site where they support it.
func (api *API) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	want := api.cfg.PublicURL.Scheme + "://" + api.cfg.PublicURL.Host
	return r.Header.Get("Origin") == want
}

func (api *API) pruneSessions(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := api.auth.PruneSessions(ctx); err != nil {
				api.log.Error("pruning sessions", "error", err)
			}
			api.pruneActivity(ctx)
		}
	}
}

// clientAddress is who a request came from: the connecting address, or with BASTION_TRUST_PROXY set,
// the last address the reverse proxy added to X-Forwarded-For (the one it saw; earlier ones could be made
// up by the client).
func (api *API) clientAddress(r *http.Request) string {
	if api.cfg.TrustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return auth.ClientAddress(r.RemoteAddr)
}

// readJSON reads a request's JSON body into v: at most 64 KB, and only the fields v has.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readJSONLimit(w, r, v, 64<<10)
}

// readJSONLimit is readJSON with a different size limit, for the few requests that carry files.
func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "send JSON")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "that request couldn't be read")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// fail answers with a problem the panel had, logging the details and showing only a general message.
func (api *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	api.log.Error("request failed", "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "something went wrong on the panel; its log has the details")
}

var errUnauthorised = errors.New("sign in first")
