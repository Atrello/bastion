package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The agent puts the panel's program in place of its own only if it's exactly the one the panel offered.
func TestSelfUpdate(t *testing.T) {
	newAgent := []byte("the new agent")
	sum := sha256.Sum256(newAgent)
	want := hex.EncodeToString(sum[:])
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/downloads/bastion-agent-linux-"+runtime.GOARCH {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(newAgent)
	}))
	defer panel.Close()
	exe := filepath.Join(t.TempDir(), "bastion-agent")
	_ = os.WriteFile(exe, []byte("the old agent"), 0o755)
	read := func() string { raw, _ := os.ReadFile(exe); return string(raw) }

	other := strings.Repeat("ab", 32)
	if err := selfUpdate(context.Background(), panel.URL, exe, other); err == nil || read() != "the old agent" {
		t.Fatalf("a program that wasn't the one offered went in: %v, %q", err, read())
	}
	if err := selfUpdate(context.Background(), panel.URL, exe, "short"); err == nil {
		t.Fatal("updated without knowing to what")
	}
	if err := selfUpdate(context.Background(), panel.URL, exe, want); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(exe); read() != "the new agent" || info.Mode().Perm() != 0o755 {
		t.Fatalf("not updated: %q", read())
	}
	if err := selfUpdate(context.Background(), panel.URL, exe, want); err == nil || !strings.Contains(err.Error(), "up to date") {
		t.Fatalf("updating again: %v", err)
	}
	// Nothing is left behind beside it.
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Fatalf("left over: %v", entries)
	}
}
