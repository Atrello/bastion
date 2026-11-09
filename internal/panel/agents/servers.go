// Package agents is the panel's side of the agents: the servers they run on, joining, their connections,
// and asking them to do things. docs/protocol.md describes what passes between them.
package agents

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
)

type Server struct {
	ID            int64
	Name          string
	Hostname      string
	OS            string
	AgentVersion  string
	DockerVersion string
	LastSeen      sql.NullString
	Joined        bool
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var serverName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,39}$`)

var ErrServerName = errors.New("a server's name is up to 40 letters, numbers, dots, dashes or underscores")

const joinTokenLife = time.Hour

// AddServer adds a server waiting for its agent, giving the one-time join token for its install command.
func (h *Hub) AddServer(ctx context.Context, name string) (*Server, string, error) {
	name = strings.TrimSpace(name)
	if !serverName.MatchString(name) {
		return nil, "", ErrServerName
	}
	token := auth.RandomToken()
	expires := time.Now().UTC().Add(joinTokenLife).Format(time.RFC3339)
	res, err := h.db.ExecContext(ctx, `INSERT INTO servers (name, join_token_hash, join_expires_at) VALUES (?, ?, ?)`, name, hash(token), expires)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, "", errors.New("there's already a server called " + name)
		}
		return nil, "", err
	}
	id, _ := res.LastInsertId()
	return &Server{ID: id, Name: name}, token, nil
}

// NewJoinToken replaces a server's join token, for joining it again (a reinstalled server, say). The old
// agent's secret stops working.
func (h *Hub) NewJoinToken(ctx context.Context, id int64) (string, error) {
	token := auth.RandomToken()
	expires := time.Now().UTC().Add(joinTokenLife).Format(time.RFC3339)
	res, err := h.db.ExecContext(ctx, `UPDATE servers SET join_token_hash = ?, join_expires_at = ?, agent_id = NULL, secret_hash = NULL WHERE id = ?`,
		hash(token), expires, id)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrNoServer
	}
	h.disconnect(id)
	return token, nil
}

var (
	ErrNoServer  = errors.New("no such server")
	ErrJoinToken = errors.New("that join command has been used or has expired: make a new one from the server's page")
)

// Enrol swaps a join token for the agent's ID and secret. The token works once.
func (h *Hub) Enrol(ctx context.Context, token, hostname string) (agentID, secret string, err error) {
	agentID, secret = auth.RandomToken()[:22], auth.RandomToken()
	now := time.Now().UTC().Format(time.RFC3339)
	if len(hostname) > 253 {
		hostname = hostname[:253]
	}
	res, err := h.db.ExecContext(ctx, `UPDATE servers
		SET agent_id = ?, secret_hash = ?, join_token_hash = NULL, join_expires_at = NULL, hostname = ?
		WHERE join_token_hash = ? AND join_expires_at > ?`, agentID, hash(secret), hostname, hash(token), now)
	if err != nil {
		return "", "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", "", ErrJoinToken
	}
	return agentID, secret, nil
}

// authenticate finds the server an agent's credentials ("<id>.<secret>") belong to.
func (h *Hub) authenticate(ctx context.Context, credentials string) (int64, error) {
	agentID, secret, ok := strings.Cut(credentials, ".")
	if !ok || agentID == "" || secret == "" {
		return 0, ErrNoServer
	}
	var id int64
	var want sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT id, secret_hash FROM servers WHERE agent_id = ?`, agentID).Scan(&id, &want)
	if err != nil || !want.Valid || subtle.ConstantTimeCompare([]byte(want.String), []byte(hash(secret))) != 1 {
		return 0, ErrNoServer
	}
	return id, nil
}

func (h *Hub) Servers(ctx context.Context) ([]Server, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, name, hostname, os, agent_version, docker_version, last_seen_at, agent_id IS NOT NULL
		FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Server
	for rows.Next() {
		var s Server
		if err := rows.Scan(&s.ID, &s.Name, &s.Hostname, &s.OS, &s.AgentVersion, &s.DockerVersion, &s.LastSeen, &s.Joined); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// DeleteServer removes a server. Its agent is disconnected and can't connect again; its stacks carry on
// running.
func (h *Hub) DeleteServer(ctx context.Context, id int64) error {
	res, err := h.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoServer
	}
	h.disconnect(id)
	return nil
}

func (h *Hub) touch(id int64) {
	_, _ = h.db.Exec(`UPDATE servers SET last_seen_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
}
