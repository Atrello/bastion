package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
	"github.com/gorilla/websocket"
)

// Live logs and container consoles for the browser: a websocket from the web app, passed through to the
// server's agent. Messages to the browser are {"type": "data" | "end", "data"}; from it, for a console,
// {"type": "input", "data"} (base64) and {"type": "resize", "cols", "rows"}.

func (api *API) streamRoutes() {
	api.mux.Handle("GET /api/streams/logs", api.signedIn(api.logsStream))
	api.mux.Handle("GET /api/streams/console", api.signedIn(api.consoleStream))
}

// browserUpgrader only lets the panel's own pages open a stream: a websocket isn't covered by the
// same-origin rules, so another site's page could otherwise open one with the session cookie.
func (api *API) browserUpgrader() *websocket.Upgrader {
	want := api.cfg.PublicURL.Scheme + "://" + api.cfg.PublicURL.Host
	return &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == want }}
}

type streamMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func (api *API) logsStream(w http.ResponseWriter, r *http.Request, u *auth.User) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("server"), 10, 64)
	args := protocol.LogsArgs{Stack: q.Get("stack"), Container: q.Get("container"), Tail: 200}
	if (args.Stack != "" && !stackNamePattern.MatchString(args.Stack)) || (args.Container != "" && !containerIDPattern.MatchString(args.Container)) {
		writeError(w, http.StatusNotFound, "no such stack or container")
		return
	}
	api.relay(w, r, id, protocol.OpLogsFollow, args, false)
}

func (api *API) consoleStream(w http.ResponseWriter, r *http.Request, u *auth.User) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("server"), 10, 64)
	container := q.Get("container")
	if !containerIDPattern.MatchString(container) {
		writeError(w, http.StatusNotFound, "no such container")
		return
	}
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	api.log.Info("console opened", "user", u.Username, "server", id, "container", container)
	api.relay(w, r, id, protocol.OpExec, protocol.ExecArgs{Container: container, Cols: uint16(min(max(cols, 20), 500)), Rows: uint16(min(max(rows, 5), 200))}, true)
}

// relay opens a stream on the agent and passes it to and from the browser until either side ends it.
func (api *API) relay(w http.ResponseWriter, r *http.Request, serverID int64, op string, args any, input bool) {
	stream, err := api.hub.OpenStream(r.Context(), serverID, op, args)
	if err != nil {
		api.agentFail(w, r, err)
		return
	}
	defer stream.Stop()
	ws, err := api.browserUpgrader().Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(64 << 10)

	// From the browser: keys and sizes for a console; for logs, only the close.
	go func() {
		defer stream.Stop()
		for {
			var m streamMessage
			if err := ws.ReadJSON(&m); err != nil {
				return
			}
			if !input {
				continue
			}
			switch m.Type {
			case "input":
				if _, err := base64.StdEncoding.DecodeString(m.Data); err == nil {
					stream.Send(protocol.OpStreamInput, protocol.StreamInput{Data: m.Data})
				}
			case "resize":
				stream.Send(protocol.OpStreamResize, protocol.StreamResize{Cols: m.Cols, Rows: m.Rows})
			}
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case part, open := <-stream.C:
			if !open || part.Stream == protocol.StreamEnd {
				_ = ws.WriteJSON(streamMessage{Type: "end"})
				return
			}
			var text string
			_ = json.Unmarshal(part.Data, &text)
			_ = ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if ws.WriteJSON(streamMessage{Type: "data", Data: text}) != nil {
				return
			}
		case <-ping.C:
			if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
