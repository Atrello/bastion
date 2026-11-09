package agent

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// Long requests (archiving, unpacking, moving) report how far they've got and stop when cancelled. A
// tracker counts what's been done, and its reader checks for cancelling as the work reads through files,
// so even one huge file stops promptly. A nil tracker does nothing, for callers that don't need one.

type tracker struct {
	ctx    context.Context
	done   atomic.Int64
	total  atomic.Int64
	report func(done, total int64) // may be nil
	last   atomic.Int64            // when it last reported, in nanoseconds
}

func newTracker(ctx context.Context, report func(done, total int64)) *tracker {
	return &tracker{ctx: ctx, report: report}
}

// add counts n more done, reporting at most twice a second.
func (t *tracker) add(n int64) {
	if t == nil {
		return
	}
	done := t.done.Add(n)
	now := time.Now().UnixNano()
	if t.report != nil && now-t.last.Load() > int64(500*time.Millisecond) {
		t.last.Store(now)
		t.report(done, t.total.Load())
	}
}

// stopped is the context's error once it's been cancelled (or run out of time).
func (t *tracker) stopped() error {
	if t == nil {
		return nil
	}
	return t.ctx.Err()
}

// reader counts what's read through it, and fails once cancelled.
func (t *tracker) reader(r io.Reader) io.Reader {
	if t == nil {
		return r
	}
	return trackedReader{r, t}
}

type trackedReader struct {
	r io.Reader
	t *tracker
}

func (tr trackedReader) Read(p []byte) (int, error) {
	if err := tr.t.stopped(); err != nil {
		return 0, err
	}
	n, err := tr.r.Read(p)
	tr.t.add(int64(n))
	return n, err
}
