package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"regexp"

	"github.com/go-webauthn/webauthn/webauthn"
)

// First-run setup: the owner's account is made with a one-time link printed in the panel's log, so only
// someone who can see the log can claim a new panel. The account is only saved once its second factor is
// set up, so there's never an account that a password alone opens.

// NewSetupToken makes a new one-time setup token (replacing any earlier one) and gives it, for the link.
func (a *Auth) NewSetupToken(ctx context.Context) (string, error) {
	token := RandomToken()
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ('setup_token', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, hashToken(token))
	return token, err
}

var ErrSetupToken = errors.New("that setup link isn't right, or the panel has been set up already. The panel's log has the current link")

func (a *Auth) checkSetupToken(ctx context.Context, token string) error {
	var want string
	err := a.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'setup_token'`).Scan(&want)
	if err != nil || subtle.ConstantTimeCompare([]byte(want), []byte(hashToken(token))) != 1 {
		return ErrSetupToken
	}
	if has, err := a.HasUsers(ctx); err != nil || has {
		return ErrSetupToken
	}
	return nil
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,31}$`)

var ErrUsername = errors.New("a username is 2 to 32 letters, numbers, dots, dashes or underscores")

// StartSetup checks the setup link and the chosen username and password, giving the account-to-be. It
// isn't saved until FinishSetup.
func (a *Auth) StartSetup(ctx context.Context, token, username, password string) (*User, error) {
	if err := a.checkSetupToken(ctx, token); err != nil {
		return nil, err
	}
	if !usernamePattern.MatchString(username) {
		return nil, ErrUsername
	}
	if err := CheckPasswordStrength(password); err != nil {
		return nil, err
	}
	return &User{Username: username, hash: HashPassword(password), WebAuthnID: randomBytes(64)}, nil
}

// FinishSetup saves the owner's account with its first second factor (an authenticator app's secret, or
// a passkey), uses up the setup link, and gives the account's ID and, with an authenticator app, its
// recovery codes.
func (a *Auth) FinishSetup(ctx context.Context, token string, u *User, totpSecret string, passkey *webauthn.Credential) (int64, []string, error) {
	if err := a.checkSetupToken(ctx, token); err != nil {
		return 0, nil, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	var secret any
	if totpSecret != "" {
		secret = totpSecret
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, totp_secret, webauthn_id) VALUES (?, ?, ?, ?)`,
		u.Username, u.hash, secret, u.WebAuthnID)
	if err != nil {
		return 0, nil, err
	}
	id, _ := res.LastInsertId()
	if passkey != nil {
		if err := a.SavePasskey(ctx, tx, id, "First passkey", passkey); err != nil {
			return 0, nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'setup_token'`); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	var codes []string
	if totpSecret != "" {
		codes, err = a.NewRecoveryCodes(ctx, id)
	}
	return id, codes, err
}

// CheckPasswordFor checks a sign-in's username and password, giving the user. A wrong name and a wrong
// password look the same, and take the same time.
func (a *Auth) CheckPasswordFor(ctx context.Context, username, password string) (*User, error) {
	u, err := a.userByName(ctx, username)
	if err != nil {
		CheckPassword(dummyHash, password)
		return nil, ErrWrongPassword
	}
	if !CheckPassword(u.hash, password) {
		return nil, ErrWrongPassword
	}
	return u, nil
}
