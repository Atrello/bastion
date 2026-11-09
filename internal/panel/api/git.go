package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// Git-backed stacks: a stack follows a compose file in a Git repository. The panel makes a deploy key for
// each, checks the branch every 5 minutes, and deploys new commits when asked (or by itself, if turned on).
// The .env file stays on the server, as always.

func (api *API) gitRoutes() {
	m := api.mux
	m.Handle("GET /api/servers/{id}/stacks/{name}/git", api.owner(api.gitStatus))
	m.Handle("PUT /api/servers/{id}/stacks/{name}/git", api.owner(api.gitSet))
	m.Handle("DELETE /api/servers/{id}/stacks/{name}/git", api.owner(api.gitUnlink))
	m.Handle("POST /api/servers/{id}/stacks/{name}/git/deploy", api.owner(api.gitDeploy))
	m.Handle("POST /api/servers/{id}/stacks", api.owner(api.createStack))
}

type gitStack struct {
	Repo           string  `json:"repo"`
	Branch         string  `json:"branch"`
	Path           string  `json:"path"`
	AutoDeploy     bool    `json:"autoDeploy"`
	PublicKey      string  `json:"publicKey"`
	DeployedCommit string  `json:"deployedCommit"`
	RemoteCommit   string  `json:"remoteCommit"`
	CheckedAt      *string `json:"checkedAt"`
	Error          string  `json:"error"`
	privateKey     string
}

func (api *API) loadGit(ctx context.Context, serverID int64, stack string) (*gitStack, error) {
	var g gitStack
	err := api.db.QueryRowContext(ctx, `SELECT repo, branch, path, auto_deploy, public_key, private_key, deployed_commit,
		remote_commit, checked_at, error FROM git_stacks WHERE server_id = ? AND stack = ?`, serverID, stack).
		Scan(&g.Repo, &g.Branch, &g.Path, &g.AutoDeploy, &g.PublicKey, &g.privateKey, &g.DeployedCommit, &g.RemoteCommit, &g.CheckedAt, &g.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &g, err
}

func (g *gitStack) args(stack string) protocol.GitArgs {
	return protocol.GitArgs{Stack: stack, Repo: g.Repo, Branch: g.Branch, Path: g.Path, Key: g.privateKey}
}

// newDeployKey makes an ed25519 key pair: the private half in OpenSSH's format, the public half as a line
// for the repository's deploy keys.
func newDeployKey(comment string) (private, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(block)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + comment, nil
}

func (api *API) gitStatus(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	g, err := api.loadGit(r.Context(), id, name)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	writeJSON(w, g) // null when the stack doesn't follow a repository
}

// gitSet links a stack to a repository (or changes the link), making its deploy key the first time.
func (api *API) gitSet(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Repo, Branch, Path string
		AutoDeploy         bool
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Repo, body.Branch, body.Path = strings.TrimSpace(body.Repo), strings.TrimSpace(body.Branch), strings.TrimSpace(body.Path)
	if body.Repo == "" || body.Branch == "" || body.Path == "" || len(body.Repo) > 500 || len(body.Path) > 300 {
		writeError(w, http.StatusBadRequest, "give the repository's address, its branch and the compose file's path in it")
		return
	}
	existing, err := api.loadGit(r.Context(), id, name)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if existing == nil {
		private, public, err := newDeployKey("bastion-" + name)
		if err != nil {
			api.fail(w, r, err)
			return
		}
		_, err = api.db.ExecContext(r.Context(), `INSERT INTO git_stacks (server_id, stack, repo, branch, path, auto_deploy, private_key, public_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, name, body.Repo, body.Branch, body.Path, body.AutoDeploy, private, public)
		if err != nil {
			api.fail(w, r, err)
			return
		}
	} else if _, err := api.db.ExecContext(r.Context(), `UPDATE git_stacks SET repo = ?, branch = ?, path = ?, auto_deploy = ?, error = ''
		WHERE server_id = ? AND stack = ?`, body.Repo, body.Branch, body.Path, body.AutoDeploy, id, name); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("git stack set", "user", u.Username, "server", id, "stack", name, "repo", body.Repo)
	g, _ := api.loadGit(r.Context(), id, name)
	api.checkGit(r.Context(), id, name, g) // see straight away whether the panel can reach it
	g, _ = api.loadGit(r.Context(), id, name)
	writeJSON(w, g)
}

func (api *API) gitUnlink(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM git_stacks WHERE server_id = ? AND stack = ?`, id, name); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// gitDeploy brings the stack's compose file up to the branch and starts it.
func (api *API) gitDeploy(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, name, ok := stackTarget(w, r)
	if !ok {
		return
	}
	g, err := api.loadGit(r.Context(), id, name)
	if err != nil || g == nil {
		writeError(w, http.StatusNotFound, "this stack doesn't follow a repository")
		return
	}
	result, err := api.deployGit(r.Context(), id, name, g)
	api.log.Info("git deploy", "user", u.Username, "server", id, "stack", name, "ok", err == nil)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, result)
}

func (api *API) deployGit(ctx context.Context, serverID int64, stack string, g *gitStack) (protocol.ActionResult, error) {
	var synced protocol.GitResult
	var result protocol.ActionResult
	err := api.hub.Request(ctx, serverID, protocol.OpGitSync, g.args(stack), &synced)
	if err == nil {
		err = api.hub.Request(ctx, serverID, protocol.OpStackAction, protocol.StackActionArgs{Name: stack, Action: "up"}, &result)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	_, _ = api.db.ExecContext(ctx, `UPDATE git_stacks SET deployed_commit = CASE WHEN ? != '' THEN ? ELSE deployed_commit END,
		remote_commit = CASE WHEN ? != '' THEN ? ELSE remote_commit END, error = ? WHERE server_id = ? AND stack = ?`,
		synced.Commit, synced.Commit, synced.Commit, synced.Commit, message, serverID, stack)
	return result, err
}

// checkGit asks the stack's agent which commit the branch is on now, deploying it if it's new and the
// stack deploys by itself.
func (api *API) checkGit(ctx context.Context, serverID int64, stack string, g *gitStack) {
	if g == nil || !api.hub.Live(serverID).Online {
		return
	}
	var head protocol.GitResult
	err := api.hub.Request(ctx, serverID, protocol.OpGitHead, g.args(stack), &head)
	message := ""
	if err != nil {
		message = err.Error()
	}
	_, _ = api.db.ExecContext(ctx, `UPDATE git_stacks SET remote_commit = CASE WHEN ? != '' THEN ? ELSE remote_commit END,
		checked_at = ?, error = ? WHERE server_id = ? AND stack = ?`,
		head.Commit, head.Commit, time.Now().UTC().Format(time.RFC3339), message, serverID, stack)
	if err == nil && g.AutoDeploy && head.Commit != g.DeployedCommit {
		if _, err := api.deployGit(ctx, serverID, stack, g); err != nil {
			api.log.Warn("git auto-deploy failed", "server", serverID, "stack", stack, "error", err)
		} else {
			api.log.Info("git auto-deployed", "server", serverID, "stack", stack, "commit", head.Commit)
		}
	}
}

// watchGit checks every Git-backed stack every 5 minutes.
func (api *API) watchGit(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		rows, err := api.db.QueryContext(ctx, `SELECT server_id, stack FROM git_stacks`)
		if err != nil {
			continue
		}
		type key struct {
			server int64
			stack  string
		}
		var all []key
		for rows.Next() {
			var k key
			if rows.Scan(&k.server, &k.stack) == nil {
				all = append(all, k)
			}
		}
		rows.Close()
		for _, k := range all {
			if g, err := api.loadGit(ctx, k.server, k.stack); err == nil {
				api.checkGit(ctx, k.server, k.stack, g)
			}
		}
	}
}

// createStack makes a new stack on a server from a compose file (and .env), and starts it.
func (api *API) createStack(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body protocol.StackSaveArgs
	if !readJSONLimit(w, r, &body, 2<<20) {
		return
	}
	if !stackNamePattern.MatchString(body.Name) {
		writeError(w, http.StatusBadRequest, "a stack's name is lower-case letters, numbers, - and _")
		return
	}
	var result protocol.ActionResult
	err := api.hub.Request(r.Context(), id, protocol.OpStackCreate, body, &result)
	api.log.Info("stack created", "user", u.Username, "server", id, "stack", body.Name, "ok", err == nil)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	writeJSON(w, result)
}
