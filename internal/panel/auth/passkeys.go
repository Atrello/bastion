package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Passkeys (WebAuthn). They're registered as discoverable credentials with user verification required
// (a fingerprint, face or PIN), so one counts as both factors: a passkey alone can sign in.

var ErrNoPasskeys = errors.New("passkeys need the panel to be reached by a name, like https://bastion.home.lab, not an IP address")

// PasskeysAvailable says whether passkeys can be used at the panel's address.
func (a *Auth) PasskeysAvailable() bool { return a.webauthn != nil }

// webauthnUser is a user as go-webauthn needs to see them.
type webauthnUser struct {
	*User
	credentials []webauthn.Credential
}

func (u webauthnUser) WebAuthnID() []byte                         { return u.User.WebAuthnID }
func (u webauthnUser) WebAuthnName() string                       { return u.Username }
func (u webauthnUser) WebAuthnDisplayName() string                { return u.Username }
func (u webauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

type Passkey struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"createdAt"`
	LastUsed  *string `json:"lastUsedAt"`
}

func (a *Auth) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at FROM passkeys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []Passkey{}
	for rows.Next() {
		var k Passkey
		if err := rows.Scan(&k.ID, &k.Name, &k.CreatedAt, &k.LastUsed); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (a *Auth) credentials(ctx context.Context, userID int64) ([]webauthn.Credential, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT credential FROM passkeys WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var creds []webauthn.Credential
	for rows.Next() {
		var raw string
		var c webauthn.Credential
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// BeginPasskeyRegistration starts adding a passkey for the user (who may not be saved yet, during setup).
// The options go to the browser; the session is kept until FinishPasskeyRegistration.
func (a *Auth) BeginPasskeyRegistration(ctx context.Context, u *User) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	if a.webauthn == nil {
		return nil, nil, ErrNoPasskeys
	}
	existing, err := a.credentials(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	exclude := make([]protocol.CredentialDescriptor, len(existing))
	for i, c := range existing {
		exclude[i] = c.Descriptor()
	}
	return a.webauthn.BeginRegistration(webauthnUser{User: u, credentials: existing},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(exclude),
	)
}

// FinishPasskeyRegistration checks the browser's answer and gives the new credential, not yet saved.
func (a *Auth) FinishPasskeyRegistration(u *User, session webauthn.SessionData, r *http.Request) (*webauthn.Credential, error) {
	if a.webauthn == nil {
		return nil, ErrNoPasskeys
	}
	return a.webauthn.FinishRegistration(webauthnUser{User: u}, session, r)
}

// SavePasskey keeps a registered passkey for the user, under a name they chose (e.g. "Phone").
func (a *Auth) SavePasskey(ctx context.Context, tx execer, userID int64, name string, c *webauthn.Credential) error {
	if tx == nil {
		tx = a.db
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO passkeys (user_id, name, credential, credential_id) VALUES (?, ?, ?, ?)`,
		userID, name, string(raw), c.ID)
	return err
}

// DeletePasskey removes one of the user's passkeys, unless it's their only second factor.
func (a *Auth) DeletePasskey(ctx context.Context, u *User, id int64) error {
	keys, err := a.Passkeys(ctx, u.ID)
	if err != nil {
		return err
	}
	if len(keys) == 1 && !u.HasAuthenticator() {
		return errors.New("this is your only way to finish signing in: add an authenticator app or another passkey first")
	}
	_, err = a.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, u.ID)
	return err
}

// BeginPasskeySignIn starts signing in with a passkey, without saying who: the browser offers the
// passkeys it has for this panel.
func (a *Auth) BeginPasskeySignIn() (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	if a.webauthn == nil {
		return nil, nil, ErrNoPasskeys
	}
	return a.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
}

var ErrPasskey = errors.New("that passkey didn't work for this panel")

// FinishPasskeySignIn checks the browser's answer and gives the user it belongs to. The passkey's
// signature counter is updated; if it ever goes backwards the passkey may have been copied, and it's
// refused.
func (a *Auth) FinishPasskeySignIn(ctx context.Context, session webauthn.SessionData, r *http.Request) (*User, error) {
	if a.webauthn == nil {
		return nil, ErrNoPasskeys
	}
	var found *User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := a.userByWebAuthnID(ctx, userHandle)
		if err != nil {
			return nil, err
		}
		creds, err := a.credentials(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		found = u
		return webauthnUser{User: u, credentials: creds}, nil
	}
	_, cred, err := a.webauthn.FinishPasskeyLogin(handler, session, r)
	if err != nil || found == nil {
		return nil, ErrPasskey
	}
	if cred.Authenticator.CloneWarning {
		return nil, ErrPasskey
	}
	raw, _ := json.Marshal(cred)
	_, err = a.db.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE credential_id = ? AND user_id = ?`,
		string(raw), now(), cred.ID, found.ID)
	return found, err
}

// execer is a database or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
