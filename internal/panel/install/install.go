// Package install serves what a new server needs to join the panel: install.sh, and the agent's
// programs for amd64 and arm64 (from BASTION_AGENT_BINARIES, which the panel's container image fills).
package install

import (
	_ "embed"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed install.sh
var script []byte

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
