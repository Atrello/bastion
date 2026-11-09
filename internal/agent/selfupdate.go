package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Updating the agent from the panel. The panel serves the agent's program (the same one a new server
// downloads to join) and says which it is by its SHA-256. The agent fetches it from the panel it joined,
// over the same connection rules as its websocket, and swaps it in only if it's exactly that program.

// Build is the SHA-256 of this agent's own program, worked out once. Empty if it can't be read.
var Build = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	sum, err := fileSHA256(exe)
	if err != nil {
		return ""
	}
	return sum
})

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const maxAgentSize = 200 << 20 // the program is about 10 MB

// selfUpdate replaces the program at exe with the panel's, if its SHA-256 is want. The caller restarts.
func selfUpdate(ctx context.Context, panel, exe, want string) error {
	if len(want) != sha256.Size*2 {
		return errors.New("the panel didn't say which program to update to")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if sum, err := fileSHA256(exe); err == nil && sum == want {
		return errors.New("this agent is up to date already")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, panel+"/downloads/bastion-agent-linux-"+runtime.GOARCH, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the panel can't be reached for the new agent: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the panel didn't give the new agent: %s", res.Status)
	}

	// Written beside the old one, so the swap is a rename on the same disk: never half a program.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".bastion-agent-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // only does anything if it wasn't swapped in
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(res.Body, maxAgentSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fmt.Errorf("the new agent didn't download: %w", err)
	case n > maxAgentSize:
		return errors.New("the panel's agent is far too big to be one")
	case hex.EncodeToString(h.Sum(nil)) != want:
		return errors.New("what the panel sent isn't the agent it offered (the panel may have been updated meanwhile): try again")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}
