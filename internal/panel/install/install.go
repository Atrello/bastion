// Package install serves what a new server needs to join the panel: install.sh, and the agent's
// programs for amd64 and arm64 (from BASTION_AGENT_BINARIES, which the panel's container image fills).
package install

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

//go:embed install.sh
var script []byte

var (
	latestMu sync.Mutex
	latest   = map[string]string{}
)

// Latest is the SHA-256 of the agent this panel hands out for arch ("amd64" or "arm64"), or "" if it
// hasn't one. An agent whose own differs needs updating. The programs come with the panel's image and
// don't change while it runs, so each is read once.
func Latest(binaries, arch string) string {
	if arch != "amd64" && arch != "arm64" {
		return ""
	}
	latestMu.Lock()
	defer latestMu.Unlock()
	path := filepath.Join(binaries, "bastion-agent-linux-"+arch)
	if sum, ok := latest[path]; ok {
		return sum
	}
	f, err := os.Open(path)
	if err != nil {
		return "" // not cached: it may appear (a panel run from source, with the agents built later)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	latest[path] = hex.EncodeToString(h.Sum(nil))
	return latest[path]
}

// Handler serves /install.sh and /downloads/bastion-agent-linux-<arch>.
func Handler(binaries string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /install.sh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		_, _ = w.Write(script)
	})
	mux.HandleFunc("GET /downloads/{file}", func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		if file != "bastion-agent-linux-amd64" && file != "bastion-agent-linux-arm64" {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(binaries, file)
		if _, err := os.Stat(path); err != nil {
			http.Error(w, "the agent's program isn't on this panel: see docs/install.md", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, path)
	})
	return mux
}
