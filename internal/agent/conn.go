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
	// Asks for an update check now (the panel's Check for updates).
	updatesNow chan struct{}
	cfg        *Config
	log        *slog.Logger
	ws         *websocket.Conn
	writeMu    sync.Mutex
	streams    *streams
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
	go measureGames(ctx)
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

	c := &conn{cfg: cfg, log: log, ws: ws, updatesNow: make(chan struct{}, 1)}
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
		// Keys typed into a console must arrive in order, so stream input is handled here, one at a time.
		// Everything else runs on its own, so a slow pull holds nothing up.
		if m.Op == protocol.OpStreamInput || m.Op == protocol.OpStreamResize || m.Op == protocol.OpStreamStop {
			c.streams.control(m)
			continue
		}
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

// metrics is the server's use, and its games'.
func (c *conn) metrics(ctx context.Context) protocol.Metrics {
	m := readMetrics()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	m.Games = gameStatuses(ctx)
	return m
}

func (c *conn) reportMetrics(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		c.event(protocol.EventMetrics, c.metrics(ctx))
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
	go c.checkUpdates(ctx, changed)
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

// checkUpdates asks the registries about the images in use soon after connecting, then every 6 hours or
// when asked, and sends the state again with what it found.
func (c *conn) checkUpdates(ctx context.Context, changed chan<- struct{}) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-c.updatesNow:
		}
		if containers, err := listContainers(ctx); err == nil {
			images := make([]string, 0, len(containers))
			for _, ct := range containers {
				images = append(images, ct.Image)
			}
			imageUpdates.check(ctx, images)
			select {
			case changed <- struct{}{}:
			default:
			}
		}
		timer.Reset(6 * time.Hour)
	}
}

func (c *conn) handle(m protocol.Message) {
	switch m.Op {
	case protocol.OpLogsFollow, protocol.OpExec:
		if err := c.streams.start(m); err != nil {
			c.answer(m.ID, nil, err)
		}
		return
	}
	data, err := c.do(m)
	if err == nil && (strings.HasPrefix(m.Op, "stack.") || m.Op == protocol.OpContainerAction || m.Op == protocol.OpGitSync || m.Op == protocol.OpCleanup) && m.Op != protocol.OpStackFiles && m.Op != protocol.OpStackLogs {
		// What Docker runs has changed: the panel hears it before the answer, so a new stack is there at once.
		if state, serr := readState(context.Background(), c.cfg.StacksDir); serr == nil {
			c.event(protocol.EventState, state)
		}
	}
	if strings.HasPrefix(m.Op, "game.") && m.Op != protocol.OpGameFiles {
		// A game's state has changed: the panel hears it before the answer, so it never shows the old one.
		c.event(protocol.EventMetrics, c.metrics(context.Background()))
	}
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
	case protocol.OpVolumes:
		return listVolumes(ctx)
	case protocol.OpImages:
		return listImages(ctx)
	case protocol.OpCleanupPreview:
		return cleanupPreview(ctx)
	case protocol.OpCleanup:
		var a protocol.CleanupArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return cleanup(ctx, a)
	case protocol.OpGitHead, protocol.OpGitSync:
		var a protocol.GitArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		if m.Op == protocol.OpGitHead {
			return gitHead(ctx, a)
		}
		return gitSync(ctx, dir, a)
	case protocol.OpGameInstall, protocol.OpGameStart:
		var a protocol.GameSpec
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		if m.Op == protocol.OpGameInstall {
			return nil, installGame(a)
		}
		return nil, startGame(ctx, a)
	case protocol.OpGamePower:
		var a protocol.GamePowerArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return nil, powerGame(ctx, a)
	case protocol.OpGameDelete:
		var a protocol.GameRef
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return nil, deleteGame(ctx, a.ID)
	case protocol.OpGameFiles:
		var a protocol.GameFileArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return gameFiles(a)
	case protocol.OpGameBackup:
		var a protocol.GameBackupArgs
		if err := decode(m.Args, &a); err != nil {
			return nil, err
		}
		return gameBackup(ctx, a)
	case protocol.OpCheckUpdates:
		select {
		case c.updatesNow <- struct{}{}:
		default: // a check is already waiting
		}
		return nil, nil
	}
	return nil, errors.New("unknown request " + m.Op)
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.New("that request couldn't be read")
	}
	return nil
}
