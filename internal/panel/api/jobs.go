package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/panel/agents"
	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
)

// Jobs: work on a server that can take a while (archiving, unpacking or moving a game's files). Each runs
// on the panel, not in the browser, so it carries on whatever page you're on (or if you close the tab),
// shows its progress in the corner of every page (web/src/components/Tasks.svelte), and can be stopped.
// They're kept in memory: a panel restart forgets them (the agent finishes or stops what it was doing).

const (
	jobsPerUser = 5                // running at once, so nobody can pile work on a server
	jobLimit    = 6 * time.Hour    // the longest one may run
	jobKept     = 10 * time.Minute // a finished one is listed this long, unless cleared
)

type job struct {
	ID     string `json:"id"`
	Title  string `json:"title"`  // "Unpacking mods.zip"
	Detail string `json:"detail"` // "Survival SMP, /mods/"
	GameID int64  `json:"gameId"`
	State  string `json:"state"` // running, done, failed, stopped
	Done   int64  `json:"done"`
	Total  int64  `json:"total"` // 0 while not known
	Error  string `json:"error,omitempty"`

	userID  int64
	ended   time.Time
	stop    context.CancelFunc
	stopped string // what's said when it's stopped: what stopping it part-way left
}

type jobList struct {
	mu   sync.Mutex
	jobs map[string]*job
}

func (api *API) jobRoutes() {
	api.jobs = &jobList{jobs: map[string]*job{}}
	api.mux.Handle("GET /api/jobs", api.signedIn(api.listJobs))
	api.mux.Handle("DELETE /api/jobs/{id}", api.signedIn(api.stopJob))
}

var errTooManyJobs = errors.New("you have 5 things running already: wait for one to finish")

// startJob runs work on a game's server as a job for u, answering 202 with it straight away.
func (api *API) startJob(w http.ResponseWriter, u *auth.User, g *game, title, detail, stopped string, work func(ctx context.Context, opts agents.RequestOptions) error) {
	ctx, stop := context.WithTimeout(context.Background(), jobLimit)
	j := &job{ID: auth.RandomToken()[:16], Title: title, Detail: detail, GameID: g.ID, State: "running", userID: u.ID, stop: stop, stopped: stopped}

	api.jobs.mu.Lock()
	running := 0
	for _, other := range api.jobs.jobs {
		if other.userID == u.ID && other.State == "running" {
			running++
		}
	}
	if running >= jobsPerUser {
		api.jobs.mu.Unlock()
		stop()
		writeError(w, http.StatusTooManyRequests, errTooManyJobs.Error())
		return
	}
	api.jobs.jobs[j.ID] = j
	view := *j
	api.jobs.mu.Unlock()

	go func() {
		defer stop()
		err := work(ctx, agents.RequestOptions{Limit: jobLimit, Progress: func(done, total int64) {
			api.jobs.mu.Lock()
			j.Done, j.Total = done, total
			api.jobs.mu.Unlock()
		}})
		api.jobs.mu.Lock()
		defer api.jobs.mu.Unlock()
		j.ended = time.Now()
		switch {
		case err == nil:
			j.State = "done"
			if j.Total > 0 {
				j.Done = j.Total
			}
		case j.State == "stopping" || errors.Is(err, context.Canceled):
			j.State, j.Error = "stopped", j.stopped
		default:
			j.State = "failed"
			_, j.Error = api.agentProblem("job "+title, err)
		}
		api.log.Info("job ended", "user", u.Username, "job", title, "detail", detail, "state", j.State, "ok", j.State != "failed")
	}()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(view)
}

// listJobs gives the signed-in person's jobs: running ones, and those that ended in the last few minutes.
func (api *API) listJobs(w http.ResponseWriter, _ *http.Request, u *auth.User) {
	api.jobs.mu.Lock()
	list := []job{}
	for id, j := range api.jobs.jobs {
		if j.State != "running" && j.State != "stopping" && time.Since(j.ended) > jobKept {
			delete(api.jobs.jobs, id)
			continue
		}
		if j.userID == u.ID {
			list = append(list, *j)
		}
	}
	api.jobs.mu.Unlock()
	sort.Slice(list, func(a, b int) bool { return list[a].ID < list[b].ID })
	writeJSON(w, list)
}

// stopJob stops a running job, or clears an ended one from the list. Only its own person can.
func (api *API) stopJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	api.jobs.mu.Lock()
	defer api.jobs.mu.Unlock()
	j := api.jobs.jobs[r.PathValue("id")]
	if j == nil || j.userID != u.ID {
		writeError(w, http.StatusNotFound, "there's no such job")
		return
	}
	if j.State == "running" {
		j.State = "stopping"
		j.stop() // the request ends, and the hub tells the agent to stop (protocol.OpCancel)
	} else if j.State != "stopping" {
		delete(api.jobs.jobs, j.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// gameFilesJob is a job doing one of the long game file operations.
func (api *API) gameFilesJob(w http.ResponseWriter, u *auth.User, g *game, title, detail string, args protocol.GameFileArgs) {
	stopped := map[string]string{
		"unarchive": "Stopped: what was unpacked so far stays.",
		"archive":   "Stopped: the part-made archive was removed.",
		"move":      "Stopped: what had moved stays moved.",
	}[args.Op]
	api.startJob(w, u, g, title, detail, stopped, func(ctx context.Context, opts agents.RequestOptions) error {
		args.ID = g.ID
		var res protocol.GameFileResult
		return api.hub.RequestWith(ctx, g.ServerID, protocol.OpGameFiles, args, &res, opts)
	})
}
