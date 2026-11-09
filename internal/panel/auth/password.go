// Package auth is signing in to the panel: passwords, authenticator app codes, recovery codes, passkeys,
// sessions, and the limits on failed attempts. docs/security.md explains the choices.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// argon2id settings: OWASP's recommended minimum is 19 MiB and 2 passes; these are stronger and still
// take well under a second.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
)

const MinPasswordLength = 12

var ErrWeakPassword = fmt.Errorf("use a password of at least %d characters", MinPasswordLength)

func CheckPasswordStrength(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength || len(password) > 1024 {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword gives the password's argon2id hash in the usual $argon2id$v=19$m=...,t=...,p=...$salt$hash form.
func HashPassword(password string) string {
	salt := randomBytes(16)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// CheckPassword says whether password matches hash, taking the same time whether or not it does.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked against when there's no such user, so a sign-in for a name that doesn't exist takes
// as long as one that does, and can't be used to find out which names exist.
var dummyHash = HashPassword(string(randomBytes(16)))

var b64 = base64.RawStdEncoding

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(errors.New("no randomness available: " + err.Error()))
	}
	return b
}

// RandomToken is 32 random bytes as URL-safe text: for session cookies, setup links and agent secrets.
func RandomToken() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }
