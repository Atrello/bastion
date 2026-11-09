package agent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/Atrello/bastion/internal/protocol"
	"github.com/creack/pty"
)

// Streams: logs that follow, and terminals into containers. Each runs until the panel stops it, the
// command ends, or the connection drops.

const maxStreams = 32

type streams struct {
	conn *conn
	mu   sync.Mutex
	open map[string]*stream
}

type stream struct {
	cancel context.CancelFunc
	tty    *os.File // set for a terminal
}

func newStreams(c *conn) *streams { return &streams{conn: c, open: map[string]*stream{}} }

func (s *streams) start(m protocol.Message) error {
	s.mu.Lock()
	if len(s.open) >= maxStreams {
		s.mu.Unlock()
		return errors.New("too many logs and consoles open on this server: close some first")
	}
	if _, taken := s.open[m.ID]; taken || m.ID == "" {
		s.mu.Unlock()
		return errors.New("that stream's ID is already in use")
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &stream{cancel: cancel}
	s.open[m.ID] = st
	s.mu.Unlock()

	var err error
	switch m.Op {
	case protocol.OpLogsFollow:
		var a protocol.LogsArgs
		if err = decode(m.Args, &a); err == nil {
			err = s.followLogs(ctx, m.ID, a)
		}
	case protocol.OpExec:
		var a protocol.ExecArgs
		if err = decode(m.Args, &a); err == nil {
			err = s.exec(ctx, m.ID, st, a)
		}
	}
	if err != nil {
		s.finish(m.ID)
	}
	return err
}

func (s *streams) followLogs(ctx context.Context, id string, a protocol.LogsArgs) error {
	args, dir, err := logsCommand(ctx, s.conn.cfg.StacksDir, a, true)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	s.conn.answer(id, nil, nil)
	go func() {
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 64<<10), 1<<20)
		for scan.Scan() {
			s.data(id, scan.Text())
		}
		_ = cmd.Wait()
		s.end(id)
	}()
	return nil
}

// exec opens a terminal in a container, with its shell: bash if it has one, otherwise sh.
func (s *streams) exec(ctx context.Context, id string, st *stream, a protocol.ExecArgs) error {
	if !containerID.MatchString(a.Container) {
		return errors.New("that isn't a container")
	}
	cmd := exec.CommandContext(ctx, "docker", "exec", "-it", a.Container, "sh", "-c",
		"command -v bash >/dev/null 2>&1 && exec bash || exec sh")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: max(a.Cols, 20), Rows: max(a.Rows, 5)})
	if err != nil {
		return err
	}
	s.mu.Lock()
	st.tty = tty
	s.mu.Unlock()
	s.conn.answer(id, nil, nil)
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				s.data(id, base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		_ = tty.Close()
		s.end(id)
	}()
	return nil
}

func (s *streams) data(id, text string) {
	raw, _ := json.Marshal(text)
	s.conn.send(protocol.Message{ID: id, Stream: protocol.StreamData, Data: raw})
}

func (s *streams) end(id string) {
	if s.finish(id) {
		s.conn.send(protocol.Message{ID: id, Stream: protocol.StreamEnd})
	}
}

// finish forgets a stream and stops what it runs, saying whether it was still open.
func (s *streams) finish(id string) bool {
	s.mu.Lock()
	st, ok := s.open[id]
	delete(s.open, id)
	s.mu.Unlock()
	if ok {
		st.cancel()
	}
	return ok
}

func (s *streams) control(m protocol.Message) {
	s.mu.Lock()
	st := s.open[m.ID]
	s.mu.Unlock()
	if st == nil {
		return
	}
	switch m.Op {
	case protocol.OpStreamStop:
		s.end(m.ID)
	case protocol.OpStreamInput:
		var a protocol.StreamInput
		if st.tty == nil || decode(m.Args, &a) != nil {
			return
		}
		if keys, err := base64.StdEncoding.DecodeString(a.Data); err == nil {
			_, _ = st.tty.Write(keys)
		}
	case protocol.OpStreamResize:
		var a protocol.StreamResize
		if st.tty != nil && decode(m.Args, &a) == nil && a.Cols > 0 && a.Rows > 0 {
			_ = pty.Setsize(st.tty, &pty.Winsize{Cols: a.Cols, Rows: a.Rows})
		}
	}
}

func (s *streams) stopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.open))
	for id := range s.open {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.finish(id)
	}
}
