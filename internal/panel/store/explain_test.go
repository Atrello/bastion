package store

import (
	"errors"
	"strings"
	"testing"
)

// The disk's errors are said in words that say what to do; anything else is left alone.
func TestExplain(t *testing.T) {
	for raw, want := range map[string]string{
		"disk I/O error (4874)":                    "most likely full or has gone read-only",
		"database or disk is full (13)":            "is full",
		"attempt to write a readonly database (8)": "read-only",
	} {
		if got := Explain(errors.New(raw), "/data").Error(); !strings.Contains(got, want) || !strings.Contains(got, raw) {
			t.Errorf("%s: %s", raw, got)
		}
	}
	other := errors.New("no such table: games")
	if Explain(other, "/data") != other {
		t.Error("another error was changed")
	}
}
