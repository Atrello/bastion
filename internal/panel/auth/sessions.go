package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// A session is a signed-in browser. The cookie holds a random token; the database holds only its hash.

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// StartSession signs the user in on this browser, giving the token for its cookie.
func (a *Auth) StartSession(ctx context.Context, userID int64, userAgent string) (string, error) {
	token := RandomToken()
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, user_agent) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), userID, now(), now(), userAgent)
	return token, err
}

var ErrNoSession = errors.New("not signed in")

// Session finds who a session token belongs to. A session idle for longer than the limit has ended;
// one in use is kept alive (its last-seen time is updated at most once a minute, to spare the database).
func (a *Auth) Session(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrNoSession
	}
	var userID int64
	var lastSeen string
	err := a.db.QueryRowContext(ctx, `SELECT user_id, last_seen_at FROM sessions WHERE token_hash = ?`, hashToken(token)).
		Scan(&userID, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	seen, _ := time.Parse(time.RFC3339, lastSeen)
	if time.Since(seen) > a.sessionIdle {
		_ = a.EndSession(ctx, token)
		return nil, ErrNoSession
	}
	if time.Since(seen) > time.Minute {
		_, _ = a.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, now(), hashToken(token))
	}
	return a.UserByID(ctx, userID)
}

func (a *Auth) EndSession(ctx context.Context, token string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// EndOtherSessions signs the user out everywhere but the session given (which may be empty: everywhere).
func (a *Auth) EndOtherSessions(ctx context.Context, userID int64, keep string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, hashToken(keep))
	return err
}

// PruneSessions deletes sessions that have been idle past the limit. The panel runs it every hour.
func (a *Auth) PruneSessions(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-a.sessionIdle).Format(time.RFC3339)
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE last_seen_at < ?`, cutoff)
	return err
}
