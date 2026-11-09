package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pquerna/otp"
)

// Setting up the panel, signing in, and the signed-in person's account. The steps between a password and
// a second factor are kept in memory (auth.Pending) under a random key in the "step" cookie; passkey
// ceremonies and a new authenticator app's secret are kept the same way under the "ceremony" cookie.

const (
	sessionCookie  = "bastion_session"
	stepCookie     = "bastion_step"
	ceremonyCookie = "bastion_ceremony"
)

// The step someone's on before they're signed in.
type setupStep struct {
	token      string
	user       *auth.User
	totpSecret string
}

type signInStep struct {
	userID int64
}

// A passkey ceremony or a new authenticator app, in progress.
type ceremony struct {
	passkey    *webauthn.SessionData
	totpSecret string
	name       string
}

func (api *API) authRoutes() {
	m := api.mux
	m.HandleFunc("GET /api/auth/state", api.state)
	m.HandleFunc("POST /api/setup", api.setup)
	m.HandleFunc("POST /api/auth/signin", api.signIn)
	m.HandleFunc("POST /api/auth/signin/code", api.signInWithCode)
	m.HandleFunc("POST /api/auth/signin/recovery", api.signInWithRecoveryCode)
	m.HandleFunc("POST /api/auth/passkey/begin", api.passkeyBegin)
	m.HandleFunc("POST /api/auth/passkey/finish", api.passkeyFinish)
	m.HandleFunc("POST /api/auth/authenticator/begin", api.authenticatorBegin)
	m.HandleFunc("POST /api/auth/authenticator/finish", api.authenticatorFinish)
	m.HandleFunc("POST /api/auth/passkey/register/begin", api.passkeyRegisterBegin)
	m.HandleFunc("POST /api/auth/passkey/register/finish", api.passkeyRegisterFinish)
	m.HandleFunc("POST /api/auth/signout", api.signOut)

	m.Handle("GET /api/account", api.signedIn(api.account))
	m.Handle("POST /api/account/password", api.signedIn(api.changePassword))
	m.Handle("POST /api/account/recovery-codes", api.signedIn(api.newRecoveryCodes))
	m.Handle("DELETE /api/account/passkeys/{id}", api.signedIn(api.deletePasskey))
	m.Handle("POST /api/account/signout-everywhere", api.signedIn(api.signOutEverywhere))
}

// Cookies: not readable by scripts, only sent to this panel, only over HTTPS when it's served over HTTPS.
func (api *API) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/api", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: api.cfg.Secure(), SameSite: http.SameSiteStrictMode,
	})
}

func (api *API) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/api", MaxAge: -1,
		HttpOnly: true, Secure: api.cfg.Secure(), SameSite: http.SameSiteStrictMode,
	})
}

func cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// keep holds a step of signing in under a cookie for ttl.
func (api *API) keep(w http.ResponseWriter, cookieName string, value any, ttl time.Duration) error {
	key, err := api.auth.Pending.Put(value, ttl)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return err
	}
	api.setCookie(w, cookieName, key, ttl)
	return nil
}

// startSession signs someone in on this browser and clears the steps that got them there.
func (api *API) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, err := api.auth.StartSession(r.Context(), userID, r.UserAgent())
	if err != nil {
		return err
	}
	api.auth.Pending.Delete(cookie(r, stepCookie))
	api.clearCookie(w, stepCookie)
	api.setCookie(w, sessionCookie, token, api.cfg.SessionIdle)
	return nil
}

// currentUser is who's signed in on this browser, if anyone.
func (api *API) currentUser(r *http.Request) (*auth.User, error) {
	return api.auth.Session(r.Context(), cookie(r, sessionCookie))
}

type userKey struct{}

// signedIn lets anyone signed in through, handing the handler who they are. Only their own account's
// routes use it: everything else is for the owner, or for the games shared with a friend.
func (api *API) signedIn(next func(http.ResponseWriter, *http.Request, *auth.User)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := api.currentUser(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, errUnauthorised.Error())
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)), u)
	})
}

// owner only lets the panel's owner through. It's what every route uses unless it says otherwise.
func (api *API) owner(next func(http.ResponseWriter, *http.Request, *auth.User)) http.Handler {
	return api.signedIn(func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		if u.Role != auth.RoleOwner {
			writeError(w, http.StatusForbidden, "that's only for the panel's owner")
			return
		}
		next(w, r, u)
	})
}

func (api *API) state(w http.ResponseWriter, r *http.Request) {
	setupNeeded, err := api.auth.HasUsers(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	reply := map[string]any{"setupNeeded": !setupNeeded, "signedIn": false, "passkeys": api.auth.PasskeysAvailable()}
	if u, err := api.currentUser(r); err == nil {
		reply["signedIn"] = true
		reply["username"] = u.Username
		reply["role"] = u.Role
	}
	writeJSON(w, reply)
}

// setup is the first step of first-run setup: the setup link's token, a username and a password. The
// second factor comes next (authenticator/begin or passkey/register/begin); the account is saved then.
func (api *API) setup(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token, Username, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	address := api.clientAddress(r)
	if wait := api.auth.Limiter.Wait("", address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	u, err := api.auth.StartSetup(r.Context(), body.Token, strings.TrimSpace(body.Username), body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrSetupToken) {
			api.auth.Limiter.Failed("", address)
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if api.keep(w, stepCookie, &setupStep{token: body.Token, user: u}, 15*time.Minute) == nil {
		w.WriteHeader(http.StatusNoContent)
	}
}

// signIn checks a username and password. The second factor comes next: a code from the authenticator
// app, a recovery code, or a passkey.
func (api *API) signIn(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	name, address := strings.ToLower(strings.TrimSpace(body.Username)), api.clientAddress(r)
	if wait := api.auth.Limiter.Wait(name, address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	u, err := api.auth.CheckPasswordFor(r.Context(), name, body.Password)
	if err != nil {
		api.auth.Limiter.Failed(name, address)
		writeError(w, http.StatusUnauthorized, "that username or password isn't right")
		return
	}
	passkeys, err := api.auth.Passkeys(r.Context(), u.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if api.keep(w, stepCookie, &signInStep{userID: u.ID}, 5*time.Minute) == nil {
		writeJSON(w, map[string]bool{"code": u.HasAuthenticator(), "passkey": len(passkeys) > 0})
	}
}

// signInStepUser is who passed the password step on this browser, if anyone did in the last 5 minutes.
func (api *API) signInStepUser(r *http.Request) (*auth.User, bool) {
	v, ok := api.auth.Pending.Get(cookie(r, stepCookie))
	step, isSignIn := v.(*signInStep)
	if !ok || !isSignIn {
		return nil, false
	}
	u, err := api.auth.UserByID(r.Context(), step.userID)
	return u, err == nil
}

func (api *API) signInWithCode(w http.ResponseWriter, r *http.Request) {
	api.secondFactor(w, r, func(u *auth.User, code string) error { return api.auth.CheckCode(r.Context(), u, code) })
}

func (api *API) signInWithRecoveryCode(w http.ResponseWriter, r *http.Request) {
	api.secondFactor(w, r, func(u *auth.User, code string) error { return api.auth.UseRecoveryCode(r.Context(), u.ID, code) })
}

// secondFactor finishes signing in with a code, counting wrong codes towards the account's limit.
func (api *API) secondFactor(w http.ResponseWriter, r *http.Request, check func(*auth.User, string) error) {
	var body struct{ Code string }
	if !readJSON(w, r, &body) {
		return
	}
	u, ok := api.signInStepUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "start again with your username and password")
		return
	}
	name, address := strings.ToLower(u.Username), api.clientAddress(r)
	if wait := api.auth.Limiter.Wait(name, address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	if err := check(u, body.Code); err != nil {
		if !errors.Is(err, auth.ErrWrongCode) {
			api.fail(w, r, err)
			return
		}
		api.auth.Limiter.Failed(name, address)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	api.auth.Limiter.Succeeded(name)
	if err := api.startSession(w, r, u.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// passkeyBegin starts signing in with a passkey: on its own, or as the second step after a password.
func (api *API) passkeyBegin(w http.ResponseWriter, r *http.Request) {
	options, session, err := api.auth.BeginPasskeySignIn()
	if errors.Is(err, auth.ErrNoPasskeys) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if api.keep(w, ceremonyCookie, &ceremony{passkey: session}, 5*time.Minute) == nil {
		writeJSON(w, options)
	}
}

func (api *API) passkeyFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := api.takeCeremony(w, r)
	if !ok || c.passkey == nil {
		writeError(w, http.StatusBadRequest, "start again")
		return
	}
	address := api.clientAddress(r)
	if wait := api.auth.Limiter.Wait("", address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	u, err := api.auth.FinishPasskeySignIn(r.Context(), *c.passkey, r)
	if err != nil {
		api.auth.Limiter.Failed("", address)
		writeError(w, http.StatusUnauthorized, auth.ErrPasskey.Error())
		return
	}
	// After a password, the passkey must be that same person's.
	if step, ok := api.signInStepUser(r); ok && step.ID != u.ID {
		writeError(w, http.StatusUnauthorized, auth.ErrPasskey.Error())
		return
	}
	api.auth.Limiter.Succeeded(strings.ToLower(u.Username))
	if err := api.startSession(w, r, u.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// takeCeremony finds the passkey ceremony or new authenticator in progress, and ends it: each is used once.
func (api *API) takeCeremony(w http.ResponseWriter, r *http.Request) (*ceremony, bool) {
	key := cookie(r, ceremonyCookie)
	v, ok := api.auth.Pending.Get(key)
	api.auth.Pending.Delete(key)
	api.clearCookie(w, ceremonyCookie)
	c, isCeremony := v.(*ceremony)
	return c, ok && isCeremony
}

// enrolling is whose second factor is being added: the account being set up, or the signed-in user.
func (api *API) enrolling(r *http.Request) (u *auth.User, setup *setupStep, ok bool) {
	if v, found := api.auth.Pending.Get(cookie(r, stepCookie)); found {
		if s, isSetup := v.(*setupStep); isSetup {
			return s.user, s, true
		}
	}
	u, err := api.currentUser(r)
	return u, nil, err == nil
}

// confirmPassword checks a signed-in user's password again before a change to how they sign in, so a
// browser left signed in (or a stolen session) can't add its own second factor. Setup has just had it.
func (api *API) confirmPassword(w http.ResponseWriter, r *http.Request, u *auth.User, setup *setupStep, password string) bool {
	if setup != nil {
		return true
	}
	name, address := strings.ToLower(u.Username), api.clientAddress(r)
	if wait := api.auth.Limiter.Wait(name, address); wait > 0 {
		writeError(w, http.StatusTooManyRequests, tryAgain(wait))
		return false
	}
	if _, err := api.auth.CheckPasswordFor(r.Context(), u.Username, password); err != nil {
		api.auth.Limiter.Failed(name, address)
		writeError(w, http.StatusBadRequest, auth.ErrWrongPassword.Error())
		return false
	}
	return true
}

// authenticatorBegin makes a secret for a new authenticator app and gives its QR code.
func (api *API) authenticatorBegin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Password string }
	if !readJSON(w, r, &body) {
		return
	}
	u, setup, ok := api.enrolling(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorised.Error())
		return
	}
	if !api.confirmPassword(w, r, u, setup, body.Password) {
		return
	}
	secret, address, err := api.auth.NewAuthenticator(u.Username)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	qr, err := qrCode(address)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if api.keep(w, ceremonyCookie, &ceremony{totpSecret: secret}, 10*time.Minute) == nil {
		writeJSON(w, map[string]string{"secret": secret, "qrCode": qr})
	}
}

// authenticatorFinish checks the new app's first code. During setup it saves the account and signs in;
// otherwise it replaces the user's authenticator app. Either way it gives new recovery codes.
func (api *API) authenticatorFinish(w http.ResponseWriter, r *http.Request) {
	var body struct{ Code string }
	if !readJSON(w, r, &body) {
		return
	}
	u, setup, ok := api.enrolling(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorised.Error())
		return
	}
	key := cookie(r, ceremonyCookie)
	v, found := api.auth.Pending.Get(key)
	c, isCeremony := v.(*ceremony)
	if !found || !isCeremony || c.totpSecret == "" {
		writeError(w, http.StatusBadRequest, "start again: scan a new QR code")
		return
	}
	if !auth.CheckSetupCode(c.totpSecret, body.Code) {
		writeError(w, http.StatusBadRequest, "that code isn't right: check the time on your phone is set automatically")
		return
	}
	api.auth.Pending.Delete(key)
	api.clearCookie(w, ceremonyCookie)

	var codes []string
	var err error
	if setup != nil {
		var id int64
		id, codes, err = api.auth.FinishSetup(r.Context(), setup.token, setup.user, c.totpSecret, nil)
		if err == nil {
			err = api.startSession(w, r, id)
		}
	} else {
		codes, err = api.auth.SaveAuthenticator(r.Context(), u.ID, c.totpSecret)
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"recoveryCodes": codes})
}

func (api *API) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	u, setup, ok := api.enrolling(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorised.Error())
		return
	}
	if !api.confirmPassword(w, r, u, setup, body.Password) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 60 {
		writeError(w, http.StatusBadRequest, "give the passkey a name, like Phone or Laptop")
		return
	}
	options, session, err := api.auth.BeginPasskeyRegistration(r.Context(), u)
	if errors.Is(err, auth.ErrNoPasskeys) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if api.keep(w, ceremonyCookie, &ceremony{passkey: session, name: name}, 5*time.Minute) == nil {
		writeJSON(w, options)
	}
}

func (api *API) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u, setup, ok := api.enrolling(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorised.Error())
		return
	}
	c, found := api.takeCeremony(w, r)
	if !found || c.passkey == nil {
		writeError(w, http.StatusBadRequest, "start again")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	cred, err := api.auth.FinishPasskeyRegistration(u, *c.passkey, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the passkey couldn't be added: "+err.Error())
		return
	}
	if setup != nil {
		id, _, err := api.auth.FinishSetup(r.Context(), setup.token, setup.user, "", cred)
		if err == nil {
			err = api.startSession(w, r, id)
		}
		if err != nil {
			api.fail(w, r, err)
			return
		}
	} else if err := api.auth.SavePasskey(r.Context(), nil, u.ID, c.name, cred); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) signOut(w http.ResponseWriter, r *http.Request) {
	if token := cookie(r, sessionCookie); token != "" {
		if err := api.auth.EndSession(r.Context(), token); err != nil {
			api.fail(w, r, err)
			return
		}
	}
	api.clearCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) account(w http.ResponseWriter, r *http.Request, u *auth.User) {
	passkeys, err := api.auth.Passkeys(r.Context(), u.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	left, err := api.auth.RecoveryCodesLeft(r.Context(), u.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	writeJSON(w, map[string]any{
		"username": u.Username, "hasAuthenticator": u.HasAuthenticator(), "recoveryCodesLeft": left, "passkeys": passkeys,
	})
}

func (api *API) changePassword(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct{ Current, Next string }
	if !readJSON(w, r, &body) {
		return
	}
	if err := api.auth.ChangePassword(r.Context(), u, body.Current, body.Next, cookie(r, sessionCookie)); err != nil {
		if errors.Is(err, auth.ErrWrongPassword) || errors.Is(err, auth.ErrWeakPassword) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newRecoveryCodes replaces the user's recovery codes. It asks for the password again, as the codes can
// sign in.
func (api *API) newRecoveryCodes(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct{ Password string }
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := api.auth.CheckPasswordFor(r.Context(), u.Username, body.Password); err != nil {
		writeError(w, http.StatusBadRequest, auth.ErrWrongPassword.Error())
		return
	}
	codes, err := api.auth.NewRecoveryCodes(r.Context(), u.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"recoveryCodes": codes})
}

func (api *API) deletePasskey(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such passkey")
		return
	}
	if err := api.auth.DeletePasskey(r.Context(), u, id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) signOutEverywhere(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if err := api.auth.EndOtherSessions(r.Context(), u.ID, cookie(r, sessionCookie)); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// qrCode is an otpauth:// address as a QR code image, ready for an <img>.
func qrCode(address string) (string, error) {
	key, err := otp.NewKeyFromURL(address)
	if err != nil {
		return "", err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func tryAgain(wait time.Duration) string {
	return fmt.Sprintf("too many tries: wait %d minutes, then try again", int(wait.Minutes())+1)
}
