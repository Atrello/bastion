package auth

import (
	"testing"
	"time"
)

func TestPasswords(t *testing.T) {
	hash := HashPassword("correct horse battery")
	if !CheckPassword(hash, "correct horse battery") {
		t.Fatal("the right password was refused")
	}
	if CheckPassword(hash, "correct horse batterY") || CheckPassword(hash, "") || CheckPassword("not a hash", "x") {
		t.Fatal("a wrong password was accepted")
	}
	if HashPassword("same") == HashPassword("same") {
		t.Fatal("two hashes of one password are the same: the salt isn't random")
	}
	if CheckPasswordStrength("eleven char") == nil || CheckPasswordStrength("twelve chars") != nil {
		t.Fatal("the length rule is wrong")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter()
	for range accountLimit {
		if l.Wait("robin", "10.0.0.1") > 0 {
			t.Fatal("limited too soon")
		}
		l.Failed("robin", "10.0.0.1")
	}
	if l.Wait("robin", "10.0.0.2") < lockout-time.Second {
		t.Fatal("an account isn't limited after 5 failures, from any address")
	}
	l.Succeeded("robin")
	if l.Wait("robin", "10.0.0.2") > 0 {
		t.Fatal("a success didn't clear the account")
	}
	// Failures with no account name only count against the address, so they can't lock anyone else out.
	for range addressLimit {
		l.Failed("", "10.0.0.3")
	}
	if l.Wait("", "10.0.0.3") == 0 || l.Wait("", "10.0.0.4") > 0 || l.Wait("robin", "10.0.0.4") > 0 {
		t.Fatal("address limits are wrong")
	}
}

func TestPendingIsCapped(t *testing.T) {
	p := NewPending()
	for range maxPending {
		if _, err := p.Put(1, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Put(1, time.Minute); err != ErrBusy {
		t.Fatal("the pending store isn't capped")
	}
	key := "not a real key"
	if _, ok := p.Get(key); ok {
		t.Fatal("found something under a made-up key")
	}
}

func TestCodes(t *testing.T) {
	if CheckSetupCode("not base32!", "123456") || CheckSetupCode("JBSWY3DPEHPK3PXP", "12345") {
		t.Fatal("a bad secret or short code was accepted")
	}
}
