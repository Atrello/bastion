package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
)

// Servers, stacks and containers: read from what the agents last reported (agents.Hub), and changes
// passed on to the agent concerned.

func (api *API) serverRoutes() {
	m := api.mux
	m.HandleFunc("POST /api/agent/enrol", api.enrol)
	m.HandleFunc("GET /api/agent/connect", api.hub.ServeAgent)

	m.Handle("GET /api/servers", api.signedIn(api.servers))
	m.Handle("POST /api/servers", api.signedIn(api.addServer))
	m.Handle("POST /api/servers/{id}/join-command", api.signedIn(api.newJoinCommand))
	m.Handle("DELETE /api/servers/{id}", api.signedIn(api.deleteServer))
	m.Handle("POST /api/updates/check", api.signedIn(api.checkUpdates))
	m.Handle("GET /api/stacks", api.signedIn(api.stacks))
	m.Handle("GET /api/containers", api.signedIn(api.containers))
	m.Handle("GET /api/servers/{id}/stacks/{name}/files", api.signedIn(api.stackFiles))
	m.Handle("PUT /api/servers/{id}/stacks/{name}/files", api.signedIn(api.saveStack))
	m.Handle("GET /api/servers/{id}/stacks/{name}/history", api.signedIn(api.stackHistory))
	m.Handle("GET /api/servers/{id}/stacks/{name}/logs", api.signedIn(api.stackLogs))
	m.Handle("POST /api/servers/{id}/stacks/{name}/actions/{action}", api.signedIn(api.stackAction))
	m.Handle("POST /api/servers/{id}/containers/{container}/actions/{action}", api.signedIn(api.containerAction))
}

// enrol is where an agent swaps its one-time join token for its own ID and secret.
func (api *API) enrol(w http.ResponseWriter, r *http.Request) {
	var body protocol.EnrolRequest
	if !readJSON(w, r, &body) {
		return
	}
	address := api.clientAddress(r)
	if wait := api.auth.Limiter.Wait("", address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	agentID, secret, err := api.hub.Enrol(r.Context(), body.Token, body.Hostname)
	if errors.Is(err, agents.ErrJoinToken) {
		api.auth.Limiter.Failed("", address)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("a server joined", "hostname", body.Hostname, "address", address)
	writeJSON(w, protocol.EnrolReply{AgentID: agentID, Secret: secret})
}

// serverView is a server as the web app shows it (web/src/lib/types.ts, Server).
type serverView struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Hostname     string            `json:"hostname"`
	OS           string            `json:"os"`
	AgentVersion string            `json:"agentVersion"`
	Online       bool              `json:"online"`
	Joined       bool              `json:"joined"`
	LastSeen     *string           `json:"lastSeen"`
	Metrics      *protocol.Metrics `json:"metrics"`
	Stacks       int               `json:"stacks"`
	Containers   int               `json:"containers"`
}

func (api *API) servers(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	list, err := api.hub.Servers(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	views := make([]serverView, 0, len(list))
	for _, s := range list {
		live := api.hub.Live(s.ID)
		v := serverView{ID: s.ID, Name: s.Name, Hostname: s.Hostname, OS: s.OS, AgentVersion: s.AgentVersion,
			Online: live.Online, Joined: s.Joined, Metrics: live.Metrics}
		if s.LastSeen.Valid {
			v.LastSeen = &s.LastSeen.String
		}
		if live.State != nil {
			v.Stacks, v.Containers = len(live.State.Stacks), len(live.State.Containers)
		}
		views = append(views, v)
	}
	writeJSON(w, views)
}

// joinCommand is what's run on a server to install its agent and join it to this panel.
func (api *API) joinCommand(token string) string {
	panel := api.cfg.PublicURL.String()
	return "curl -fsSL " + panel + "/install.sh | sudo sh -s -- " + panel + " " + token
}

func (api *API) addServer(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var body struct{ Name string }
	if !readJSON(w, r, &body) {
		return
	}
	s, token, err := api.hub.AddServer(r.Context(), body.Name)
	if err != nil {
		if errors.Is(err, agents.ErrServerName) || strings.HasPrefix(err.Error(), "there's already") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		api.fail(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"server": serverView{ID: s.ID, Name: s.Name}, "joinCommand": api.joinCommand(token)})
}

func (api *API) newJoinCommand(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	token, err := api.hub.NewJoinToken(r.Context(), id)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"joinCommand": api.joinCommand(token)})
}

// checkUpdates asks every connected agent to check its images' registries now. The results arrive with
// each agent's next state, a few seconds later.
func (api *API) checkUpdates(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	list, err := api.hub.Servers(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	for _, s := range list {
		if api.hub.Live(s.ID).Online {
			_ = api.hub.Request(r.Context(), s.ID, protocol.OpCheckUpdates, nil, nil)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteServer(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := api.hub.DeleteServer(r.Context(), id); err != nil {
		api.agentFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stackView is a stack as the web app shows it (web/src/lib/types.ts, Stack).
type stackView struct {
	ServerID   int64  `json:"serverId"`
	ServerName string `json:"serverName"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Running    int    `json:"running"`
	Total      int    `json:"total"`
	ConfigFile string `json:"configFile"`
	HasEnv     bool   `json:"hasEnv"`
	Updates    int    `json:"updates"`
}

func stackState(s protocol.Stack) string {
	switch {
	case s.Total == 0:
		return "new"
	case s.Running == s.Total:
		return "running"
	case s.Running == 0:
		return "stopped"
	}
	return "partial"
}

func (api *API) stacks(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	list, err := api.hub.Servers(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	views := []stackView{}
	for _, s := range list {
		if state := api.hub.Live(s.ID).State; state != nil {
			for _, st := range state.Stacks {
				views = append(views, stackView{ServerID: s.ID, ServerName: s.Name, Name: st.Name, State: stackState(st),
					Running: st.Running, Total: st.Total, ConfigFile: st.ConfigFile, HasEnv: st.HasEnv, Updates: st.Updates})
			}
		}
	}
	writeJSON(w, views)
}

type containerView struct {
	protocol.Container
	ServerID int64 `json:"serverId"`
}

func (api *API) containers(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	list, err := api.hub.Servers(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	views := []containerView{}
	for _, s := range list {
		if state := api.hub.Live(s.ID).State; state != nil {
			for _, c := range state.Containers {
				views = append(views, containerView{Container: c, ServerID: s.ID})
			}
		}
	}
	writeJSON(w, views)
}

var stackNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// stackTarget reads the server and stack from the path, checking both.
func stackTarget(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return 0, "", false
	}
	name := r.PathValue("name")
	if !stackNamePattern.MatchString(name) {
		writeError(w, http.StatusNotFound, "no such stack")
		return 0, "", false
	}
	return id, name, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such server")
		return 0, false
	}
	return id, true
}

// agentFail answers with why an agent couldn't do something: offline, refused, or too slow.
func (api *API) agentFail(w http.ResponseWriter, r *http.Request, err error) {
	var agentErr *agents.AgentError
	switch {
	case errors.Is(err, agents.ErrOffline):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, agents.ErrNoServer):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &agentErr):
		writeError(w, http.StatusBadRequest, agentErr.Message)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "the server took too long to answer")
	default:
		api.fail(w, r, err)
	}
}

func (api *API) stackFiles(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var files protocol.StackFiles
	if err := api.hub.Request(r.Context(), id, protocol.OpStackFiles, protocol.StackArgs{Name: name}, &files); err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, files)
}

// saveStack saves a stack's compose file and .env on its server (Docker checks them first), keeping the
// compose file it replaces in the history.
func (api *API) saveStack(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var body protocol.StackFiles
	if !readJSONLimit(w, r, &body, 2<<20) {
		return
	}
	var before protocol.StackFiles
	if err := api.hub.Request(r.Context(), id, protocol.OpStackFiles, protocol.StackArgs{Name: name}, &before); err != nil {
		api.agentFail(w, r, err)
		return
	}
	save := protocol.StackSaveArgs{Name: name, Compose: body.Compose, Env: body.Env}
	if err := api.hub.Request(r.Context(), id, protocol.OpStackSave, save, nil); err != nil {
		api.agentFail(w, r, err)
		return
	}
	if before.Compose != body.Compose {
		if _, err := api.db.ExecContext(r.Context(), `INSERT INTO compose_versions (server_id, stack, compose, saved_by) VALUES (?, ?, ?, ?)`,
			id, name, before.Compose, u.ID); err != nil {
			api.fail(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) stackHistory(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	rows, err := api.db.QueryContext(r.Context(), `SELECT v.id, v.saved_at, COALESCE(u.username, ''), v.note
		FROM compose_versions v LEFT JOIN users u ON u.id = v.saved_by
		WHERE v.server_id = ? AND v.stack = ? ORDER BY v.id DESC LIMIT 100`, id, name)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	type version struct {
		ID      int64  `json:"id"`
		SavedAt string `json:"savedAt"`
		SavedBy string `json:"savedBy"`
		Note    string `json:"note"`
	}
	list := []version{}
	for rows.Next() {
		var v version
		if err := rows.Scan(&v.ID, &v.SavedAt, &v.SavedBy, &v.Note); err != nil {
			api.fail(w, r, err)
			return
		}
		list = append(list, v)
	}
	writeJSON(w, list)
}

func (api *API) stackLogs(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	var lines protocol.LogLines
	if err := api.hub.Request(r.Context(), id, protocol.OpStackLogs, protocol.LogsArgs{Stack: name, Tail: tail}, &lines); err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, lines)
}

var stackActions = map[string]bool{"up": true, "down": true, "start": true, "stop": true, "restart": true, "pull": true, "update": true}

func (api *API) stackAction(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	action := r.PathValue("action")
	if !stackActions[action] {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	var result protocol.ActionResult
	err := api.hub.Request(r.Context(), id, protocol.OpStackAction, protocol.StackActionArgs{Name: name, Action: action}, &result)
	api.log.Info("stack action", "user", u.Username, "server", id, "stack", name, "action", action, "ok", err == nil)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, result)
}

var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func (api *API) containerAction(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	container, action := r.PathValue("container"), r.PathValue("action")
	if !containerIDPattern.MatchString(container) || (action != "start" && action != "stop" && action != "restart") {
		writeError(w, http.StatusNotFound, "no such container or action")
		return
	}
	var result protocol.ActionResult
	err := api.hub.Request(r.Context(), id, protocol.OpContainerAction, protocol.ContainerActionArgs{ID: container, Action: action}, &result)
	api.log.Info("container action", "user", u.Username, "server", id, "container", container, "action", action, "ok", err == nil)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, result)
}
