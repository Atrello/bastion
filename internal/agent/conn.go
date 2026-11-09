package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
	"github.com/gorilla/websocket"
)

// The connection to the panel: one websocket, opened by the agent and kept open, reconnecting with a
// growing wait if it drops. Requests are answered each on their own, so a slow pull holds nothing up.

const (
	pingEvery = 25 * time.Second
	readLimit = 4 << 20 // a compose file and its .env, with room to spare
)

type conn struct {
	cfg     *Config
	log     *slog.Logger
	ws      *websocket.Conn
	writeMu sync.Mutex
	streams *streams
}

func (c *conn) send(m protocol.Message) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if err := c.ws.WriteJSON(m); err != nil {
		_ = c.ws.Close() // the read loop sees it and reconnects
	}
}

func (c *conn) event(name string, data any) {
	raw, _ := json.Marshal(data)
	c.send(protocol.Message{Event: name, Data: raw})
}

func (c *conn) answer(id string, data any, err error) {
	ok := err == nil
	m := protocol.Message{ID: id, OK: &ok}
	if err != nil {
		m.Error = err.Error()
	} else if data != nil {
		m.Data, _ = json.Marshal(data)
	}
	c.send(m)
}

// Run keeps the agent connected to the panel until ctx ends.
func Run(ctx context.Context, cfg *Config, log *slog.Logger) {
	wait := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := connect(ctx, cfg, log)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			wait = time.Second // it was up a while: try again straight away
		}
		log.Warn("disconnected from the panel", "error", err, "retrying in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, time.Minute)
	}
}

func connect(ctx context.Context, cfg *Config, log *slog.Logger) error {
	url := "ws" + strings.TrimPrefix(cfg.Panel, "http") + "/api/agent/connect"
	header := http.Header{"Authorization": {"Bearer " + cfg.AgentID + "." + cfg.Secret}}
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second, Proxy: http.ProxyFromEnvironment}
	ws, res, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		if res != nil && res.StatusCode == http.StatusUnauthorized {
			return errors.New("the panel doesn't know this agent any more: remove " + ConfigPath() + " and join again")
		}
		return err
	}
	defer ws.Close()
	log.Info("connected to the panel", "panel", cfg.Panel)

	c := &conn{cfg: cfg, log: log, ws: ws}
	c.streams = newStreams(c)
	defer c.streams.stopAll()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { // closing the socket ends the read loop when the agent stops
		<-ctx.Done()
		_ = ws.Close()
	}()

	ws.SetReadLimit(readLimit)
	_ = ws.SetReadDeadline(time.Now().Add(2 * pingEvery))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(2 * pingEvery)) })

	c.event(protocol.EventHello, hello())
	go c.ping(ctx)
	go c.reportMetrics(ctx)
	go c.reportState(ctx)

	for {
		var m protocol.Message
		if err := ws.ReadJSON(&m); err != nil {
			return err
		}
		_ = ws.SetReadDeadline(time.Now().Add(2 * pingEvery))
		go c.handle(m)
	}
}

func (c *conn) ping(ctx context.Context) {
	tick := time.NewTicker(pingEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
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

func (c *conn) reportMetrics(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		c.event(protocol.EventMetrics, readMetrics())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// reportState sends what Docker is running: now, a moment after Docker reports a change (so a burst of
// changes, like a stack starting, is one message), and every minute in case an event was missed.
func (c *conn) reportState(ctx context.Context) {
	changed := make(chan struct{}, 1)
	go watchDocker(ctx, changed)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if state, err := readState(ctx, c.cfg.StacksDir); err == nil {
			c.event(protocol.EventState, state)
		} else if ctx.Err() == nil {
			c.log.Error("reading Docker's state", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-changed:
			select { // let the burst settle
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *conn) handle(m protocol.Message) {
	switch m.Op {
	case protocol.OpStreamInput, protocol.OpStreamResize, protocol.OpStreamStop:
		c.streams.control(m)
		return
	case protocol.OpLogsFollow, protocol.OpExec:
		if err := c.streams.start(m); err != nil {
			c.answer(m.ID, nil, err)
		}
		return
	}
	data, err := c.do(m)
	c.answer(m.ID, data, err)
}

// do runs one request. A pull or an update can take a while, so each has up to 15 minutes.
func (c *conn) do(m protocol.Message) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	dir := c.cfg.StacksDir
	switch m.Op {
	case protocol.OpStackFiles:
		var a protocol.StackArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return stackFiles(ctx, dir, a)
	case protocol.OpStackSave:
		var a protocol.StackSaveArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return nil, saveStack(ctx, dir, a)
	case protocol.OpStackCreate:
		var a protocol.StackSaveArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return createStack(ctx, dir, a)
	case protocol.OpStackAction:
		var a protocol.StackActionArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return stackAction(ctx, dir, a)
	case protocol.OpStackLogs:
		var a protocol.LogsArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return stackLogs(ctx, dir, a)
	case protocol.OpContainerAction:
		var a protocol.ContainerActionArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return containerAction(ctx, a)
	}
	return nil, errors.New("unknown request " + m.Op)
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.New("that request couldn't be read")
	}
	return nil
}
