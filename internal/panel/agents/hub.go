package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/protocol"
	"github.com/gorilla/websocket"
)

// Hub holds the agents' connections and the latest of what each one has said: its hello, its metrics and
// what Docker is running. Pages read from here, so they never wait on the servers.
type Hub struct {
	db    *sql.DB
	log   *slog.Logger
	mu    sync.RWMutex
	conns map[int64]*agentConn
}

func NewHub(db *sql.DB, log *slog.Logger) *Hub {
	return &Hub{db: db, log: log, conns: map[int64]*agentConn{}}
}

type agentConn struct {
	serverID int64
	ws       *websocket.Conn
	writeMu  sync.Mutex
	done     chan struct{}

	mu      sync.Mutex
	hello   *protocol.Hello
	state   *protocol.State
	metrics *protocol.Metrics
	// Requests waiting for their answer, and streams open, by ID.
	waiting map[string]chan protocol.Message
	streams map[string]chan protocol.Message
	// Where progress on a request goes (RequestOptions.Progress), by its ID.
	progress map[string]func(done, total int64)
}

// Live is what the hub knows about a server right now.
type Live struct {
	Online  bool
	Hello   *protocol.Hello
	State   *protocol.State
	Metrics *protocol.Metrics
}

func (h *Hub) Live(serverID int64) Live {
	h.mu.RLock()
	c := h.conns[serverID]
	h.mu.RUnlock()
	if c == nil {
		return Live{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return Live{Online: true, Hello: c.hello, State: c.state, Metrics: c.metrics}
}

const (
	pingEvery    = 25 * time.Second
	readLimit    = 8 << 20 // a state message for a server with many containers, with room to spare
	requestLimit = 15 * time.Minute
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  16 << 10,
	WriteBufferSize: 16 << 10,
	// Agents aren't browsers and send no Origin. Refusing any that do means no web page can open this
	// connection on someone's behalf.
	CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
}

// ServeAgent is the agents' websocket: GET /api/agent/connect with "Authorization: Bearer <id>.<secret>".
func (h *Hub) ServeAgent(w http.ResponseWriter, r *http.Request) {
	credentials, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	serverID, err := h.authenticate(r.Context(), credentials)
	if !ok || err != nil {
		http.Error(w, "unknown agent", http.StatusUnauthorized)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader has answered
	}
	c := &agentConn{
		serverID: serverID, ws: ws, done: make(chan struct{}),
		waiting: map[string]chan protocol.Message{}, streams: map[string]chan protocol.Message{},
		progress: map[string]func(done, total int64){},
	}
	h.mu.Lock()
	old := h.conns[serverID]
	h.conns[serverID] = c
	h.mu.Unlock()
	if old != nil { // the same agent reconnecting: the old connection is finished
		_ = old.ws.Close()
	}
	h.touch(serverID)
	h.log.Info("agent connected", "server", serverID, "address", auth.ClientAddress(r.RemoteAddr))

	go h.ping(c)
	h.read(c)

	// Anyone watching this agent's logs or terminals is told it's ended.
	c.mu.Lock()
	for id, ch := range c.streams {
		close(ch)
		delete(c.streams, id)
	}
	c.mu.Unlock()

	h.mu.Lock()
	if h.conns[serverID] == c {
		delete(h.conns, serverID)
	}
	h.mu.Unlock()
	close(c.done)
	_ = ws.Close()
	h.touch(serverID)
	h.log.Info("agent disconnected", "server", serverID)
}

func (h *Hub) ping(c *agentConn) {
	tick := time.NewTicker(pingEvery)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tick.C:
			c.writeMu.Lock()
			err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			c.writeMu.Unlock()
			if err != nil {
				_ = c.ws.Close()
				return
			}
		}
	}
}

func (h *Hub) read(c *agentConn) {
	c.ws.SetReadLimit(readLimit)
	_ = c.ws.SetReadDeadline(time.Now().Add(2 * pingEvery))
	c.ws.SetPongHandler(func(string) error { return c.ws.SetReadDeadline(time.Now().Add(2 * pingEvery)) })
	for {
		var m protocol.Message
		if err := c.ws.ReadJSON(&m); err != nil {
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(2 * pingEvery))
		switch {
		case m.Event != "":
			h.event(c, m)
		case m.Stream != "":
			c.mu.Lock()
			ch := c.streams[m.ID]
			if m.Stream == protocol.StreamEnd {
				delete(c.streams, m.ID)
			}
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default: // the reader has fallen behind: drop rather than hold up every other message
				}
				if m.Stream == protocol.StreamEnd {
					close(ch)
				}
			}
		case m.OK != nil:
			c.mu.Lock()
			ch := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

func (h *Hub) event(c *agentConn, m protocol.Message) {
	if m.Event == protocol.EventProgress {
		var p protocol.Progress
		if json.Unmarshal(m.Data, &p) == nil {
			c.mu.Lock()
			report := c.progress[p.ID]
			c.mu.Unlock()
			if report != nil {
				report(p.Done, p.Total)
			}
		}
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch m.Event {
	case protocol.EventHello:
		var hello protocol.Hello
		if json.Unmarshal(m.Data, &hello) == nil {
			c.hello = &hello
			_, _ = h.db.Exec(`UPDATE servers SET hostname = ?, os = ?, agent_version = ?, docker_version = ? WHERE id = ?`,
				clip(hello.Hostname), clip(hello.OS), clip(hello.Version), clip(hello.Docker), c.serverID)
		}
	case protocol.EventState:
		var state protocol.State
		if json.Unmarshal(m.Data, &state) == nil {
			c.state = &state
		}
	case protocol.EventMetrics:
		var metrics protocol.Metrics
		if json.Unmarshal(m.Data, &metrics) == nil {
			c.metrics = &metrics
		}
	}
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

var ErrOffline = errors.New("that server's agent isn't connected")

func (h *Hub) conn(serverID int64) (*agentConn, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c := h.conns[serverID]; c != nil {
		return c, nil
	}
	return nil, ErrOffline
}

func (c *agentConn) send(m protocol.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return c.ws.WriteJSON(m)
}

// Request asks a server's agent to do something and waits for its answer, decoding it into result.
func (h *Hub) Request(ctx context.Context, serverID int64, op string, args, result any) error {
	return h.RequestWith(ctx, serverID, op, args, result, RequestOptions{})
}

// RequestOptions are for long requests: archiving, unpacking or moving a game's files.
type RequestOptions struct {
	// How long to wait for the answer (15 minutes if 0).
	Limit time.Duration
	// Called as the agent reports how far it's got.
	Progress func(done, total int64)
}

// RequestWith is Request with options. If ctx ends first the agent is told to stop (OpCancel).
func (h *Hub) RequestWith(ctx context.Context, serverID int64, op string, args, result any, opts RequestOptions) error {
	c, err := h.conn(serverID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	id := auth.RandomToken()[:16]
	answer := make(chan protocol.Message, 1)
	c.mu.Lock()
	c.waiting[id] = answer
	if opts.Progress != nil {
		c.progress[id] = opts.Progress
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		delete(c.progress, id)
		c.mu.Unlock()
	}()

	if err := c.send(protocol.Message{ID: id, Op: op, Args: raw}); err != nil {
		return ErrOffline
	}
	limit := opts.Limit
	if limit == 0 {
		limit = requestLimit
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	select {
	case m := <-answer:
		if !*m.OK {
			return &AgentError{m.Error}
		}
		if result != nil && len(m.Data) > 0 {
			return json.Unmarshal(m.Data, result)
		}
		return nil
	case <-c.done:
		return ErrOffline
	case <-ctx.Done():
		stop, _ := json.Marshal(protocol.CancelArgs{ID: id})
		_ = c.send(protocol.Message{ID: auth.RandomToken()[:16], Op: protocol.OpCancel, Args: stop})
		return ctx.Err()
	}
}

// AgentError is a request the agent refused or that failed on the server, like a compose file Docker
// says is wrong. Its message is meant for the person who asked.
type AgentError struct{ Message string }

func (e *AgentError) Error() string { return e.Message }

// Stream is a stream open on an agent: live logs or a terminal. Parts arrive on C until it's closed.
type Stream struct {
	C        <-chan protocol.Message
	id       string
	conn     *agentConn
	stopOnce sync.Once
}

// OpenStream starts a stream on a server's agent.
func (h *Hub) OpenStream(ctx context.Context, serverID int64, op string, args any) (*Stream, error) {
	c, err := h.conn(serverID)
	if err != nil {
		return nil, err
	}
	id := auth.RandomToken()[:16]
	parts := make(chan protocol.Message, 256)
	c.mu.Lock()
	c.streams[id] = parts
	c.mu.Unlock()
	s := &Stream{C: parts, id: id, conn: c}
	// The agent answers once the stream has started (or says why it couldn't).
	if err := h.requestOn(ctx, c, id, op, args); err != nil {
		c.mu.Lock()
		delete(c.streams, id)
		c.mu.Unlock()
		return nil, err
	}
	return s, nil
}

func (h *Hub) requestOn(ctx context.Context, c *agentConn, id, op string, args any) error {
	raw, _ := json.Marshal(args)
	answer := make(chan protocol.Message, 1)
	c.mu.Lock()
	c.waiting[id] = answer
	c.mu.Unlock()
	if err := c.send(protocol.Message{ID: id, Op: op, Args: raw}); err != nil {
		return ErrOffline
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case m := <-answer:
		if !*m.OK {
			return &AgentError{m.Error}
		}
		return nil
	case <-c.done:
		return ErrOffline
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send passes input to the stream (keys typed into a terminal, or its new size).
func (s *Stream) Send(op string, args any) {
	raw, _ := json.Marshal(args)
	_ = s.conn.send(protocol.Message{ID: s.id, Op: op, Args: raw})
}

// Stop ends the stream on the agent.
func (s *Stream) Stop() {
	s.stopOnce.Do(func() { _ = s.conn.send(protocol.Message{ID: s.id, Op: protocol.OpStreamStop}) })
}

// disconnect closes a server's agent connection, if it has one.
func (h *Hub) disconnect(serverID int64) {
	h.mu.RLock()
	c := h.conns[serverID]
	h.mu.RUnlock()
	if c != nil {
		_ = c.ws.Close()
	}
}
