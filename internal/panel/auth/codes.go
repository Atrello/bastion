package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Authenticator app codes (TOTP, 30-second steps, 6 digits) and recovery codes.

var ErrWrongCode = errors.New("that code isn't right")

// NewAuthenticator makes a secret for an authenticator app, with the otpauth:// address its QR code holds.
func (a *Auth) NewAuthenticator(username string) (secret, address string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Bastion (" + a.origin.Host + ")",
		AccountName: username,
		SecretSize:  20,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// codeStep is the 30-second step a code is valid for, checking the step before and after too (a clock
// a little out). Zero means the code isn't right.
func codeStep(secret, code string, at time.Time) int64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0
	}
	step := at.Unix() / 30
	for _, s := range []int64{step, step - 1, step + 1} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s
		}
	}
	return 0
}

// CheckSetupCode checks the first code from a newly added authenticator app, before its secret is saved.
func CheckSetupCode(secret, code string) bool {
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret); err != nil {
		return false
	}
	return codeStep(secret, code, time.Now()) != 0
}

// CheckCode checks a code from the user's authenticator app. Each code works once: a code (or an earlier
// one) that's already been used is refused, so one seen over someone's shoulder can't be used again.
func (a *Auth) CheckCode(ctx context.Context, u *User, code string) error {
	if !u.totpSecret.Valid {
		return ErrWrongCode
	}
	step := codeStep(u.totpSecret.String, code, time.Now())
	if step == 0 || step <= u.totpStep {
		return ErrWrongCode
	}
	// Only one sign-in can claim this step, even if two arrive at once.
	res, err := a.db.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, u.ID, step)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWrongCode
	}
	return nil
}

// SaveAuthenticator keeps the user's authenticator app secret (after its first code has been checked)
// and gives them new recovery codes, replacing any they had.
func (a *Auth) SaveAuthenticator(ctx context.Context, userID int64, secret string) ([]string, error) {
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET totp_secret = ?, totp_last_step = 0 WHERE id = ?`, secret, userID); err != nil {
		return nil, err
	}
	return a.NewRecoveryCodes(ctx, userID)
}

// NewRecoveryCodes gives the user ten single-use codes, replacing any earlier ones. They're shown once.
func (a *Auth) NewRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		// 10 characters from a 32-letter alphabet without look-alikes: 50 bits each, shown as xxxxx-xxxxx.
		raw := strings.ToLower(base32.StdEncoding.EncodeToString(randomBytes(10))[:10])
		codes[i] = raw[:5] + "-" + raw[5:]
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, hashToken(codes[i])); err != nil {
			return nil, err
		}
	}
	return codes, tx.Commit()
}

// UseRecoveryCode signs in with one of the user's recovery codes, which then can't be used again.
func (a *Auth) UseRecoveryCode(ctx context.Context, userID int64, code string) error {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) == 10 {
		code = code[:5] + "-" + code[5:]
	}
	res, err := a.db.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, now(), userID, hashToken(code))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWrongCode
	}
	return nil
}

// RecoveryCodesLeft is how many unused recovery codes the user has.
func (a *Auth) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}
