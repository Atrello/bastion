package api

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"
)

// Long jobs on an agent (unpacking a big archive, say) can take minutes, and a proxy in front of the panel
// gives up on a request that's quiet for a minute or so (Nginx and NPM after 60 seconds, Cloudflare after
// 100), showing an error though the job carries on. slowly keeps such a request going: if the job isn't
// done in a few seconds it answers 200 with the header X-Bastion-Slow, sends a space every 15 seconds,
// and then the outcome as JSON: {} or {"error": "...", "status": N}. lib/api.ts reads it either way.

// Variables so the tests can shorten them.
var slowAfter, keepAlive = 5 * time.Second, 15 * time.Second

func (api *API) slowly(w http.ResponseWriter, r *http.Request, job func(ctx context.Context) error) {
	done := make(chan error, 1)
	// The job runs to the end even if the browser goes, so its outcome is still logged.
	ctx := context.WithoutCancel(r.Context())
	go func() { done <- job(ctx) }()

	var err error
	select {
	case err = <-done:
		if err != nil {
			api.agentFail(w, r, err)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	case <-time.After(slowAfter):
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Bastion-Slow", "1")
	w.WriteHeader(http.StatusOK)
	flusher := http.NewResponseController(w)
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	for waiting := true; waiting; {
		_, _ = w.Write([]byte(" "))
		_ = flusher.Flush()
		select {
		case err = <-done:
			waiting = false
		case <-ticker.C:
		}
	}
	if err == nil {
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	// The same words and status agentFail would have answered with, inside the 200.
	answer := &captured{header: http.Header{}}
	api.agentFail(answer, r, err)
	body := bytes.TrimSpace(answer.body.Bytes())
	if len(body) > 1 && body[0] == '{' {
		body = append([]byte(`{"status":`+strconv.Itoa(answer.status)+`,`), body[1:]...)
	}
	_, _ = w.Write(append(body, '\n'))
}

// captured is an answer kept rather than sent.
type captured struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captured) Header() http.Header         { return c.header }
func (c *captured) Write(b []byte) (int, error) { return c.body.Write(b) }
func (c *captured) WriteHeader(status int)      { c.status = status }
