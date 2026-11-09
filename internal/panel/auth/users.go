package auth

import (
	"context"
	"database/sql"
	"errors"
)

type User struct {
	ID         int64
	Username   string
	hash       string
	totpSecret sql.NullString
	totpStep   int64
	WebAuthnID []byte
}

// HasAuthenticator says whether the user has set up an authenticator app.
func (u *User) HasAuthenticator() bool { return u.totpSecret.Valid }

var ErrNoUser = errors.New("no such user")

const userColumns = `id, username, password_hash, totp_secret, totp_last_step, webauthn_id`

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.hash, &u.totpSecret, &u.totpStep, &u.WebAuthnID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoUser
	}
	return &u, err
}

func (a *Auth) userByName(ctx context.Context, name string) (*User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, name))
}

func (a *Auth) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (a *Auth) userByWebAuthnID(ctx context.Context, handle []byte) (*User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE webauthn_id = ?`, handle))
}

// HasUsers says whether anyone can sign in yet; until then the panel offers its first-run setup.
func (a *Auth) HasUsers(ctx context.Context) (bool, error) {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n > 0, err
}

// ChangePassword checks the current password, sets the new one, and signs out every other session.
func (a *Auth) ChangePassword(ctx context.Context, u *User, current, next, keepSession string) error {
	if !CheckPassword(u.hash, current) {
		return ErrWrongPassword
	}
	if err := CheckPasswordStrength(next); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, HashPassword(next), u.ID); err != nil {
		return err
	}
	return a.EndOtherSessions(ctx, u.ID, keepSession)
}

var ErrWrongPassword = errors.New("that password isn't right")
