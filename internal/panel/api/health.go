package api

import (
	"fmt"
	"net/http"
	"syscall"

	"github.com/Atrello/bastion/internal/panel/store"
)

// The panel's own health: whether it can write its database, and how full the disk under it is. Docker's
// health check asks (bastion healthcheck, in the image), and the alerts warn before that disk fills.

func (api *API) healthRoutes() {
	// Signed out on purpose: it says only whether the panel works, nothing about what's in it.
	api.mux.HandleFunc("GET /api/health", api.health)
}

func (api *API) health(w http.ResponseWriter, r *http.Request) {
	// A write that changes nothing: it fails the same way a real one would on a full or read-only disk.
	if _, err := api.db.ExecContext(r.Context(), `UPDATE settings SET value = value WHERE key = 'alert_kinds'`); err != nil {
		writeError(w, http.StatusServiceUnavailable, store.Explain(err, api.cfg.DataDir).Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// panelDiskUse is how full the disk under the panel's data is, in per cent, or -1 if it can't be told.
func (api *API) panelDiskUse() int {
	var fs syscall.Statfs_t
	if syscall.Statfs(api.cfg.DataDir, &fs) != nil || fs.Blocks == 0 {
		return -1
	}
	return int(100 - fs.Bavail*100/fs.Blocks)
}

// panelProblem is the alert for the panel's own disk filling, if it is.
func (api *API) panelProblem() *problem {
	if use := api.panelDiskUse(); use >= 90 {
		return &problem{"disk:panel", "disk", fmt.Sprintf("The panel's disk is %d%% full", use),
			"Bastion stops working when it fills. Free space on the host (docker builder prune -a and docker image prune -a usually help)."}
	}
	return nil
}
