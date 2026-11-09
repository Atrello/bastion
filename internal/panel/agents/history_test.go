package agents

import (
	"testing"

	"github.com/Atrello/bastion/internal/protocol"
)

// Running games' use is kept, at most every few seconds and for ten minutes; stopped games' isn't.
func TestHistory(t *testing.T) {
	h := &Hub{}
	m := &protocol.Metrics{Games: []protocol.GameStatus{{ID: 1, State: "running", CPUPercent: 50, NetIn: 100}, {ID: 2, State: "stopped"}}}
	h.history.record(m)
	h.history.record(m) // straight after: not kept twice
	if got := h.GameHistory(1); len(got) != 1 || got[0].CPU != 50 || got[0].NetIn != 100 {
		t.Fatalf("game 1: %+v", got)
	}
	if got := h.GameHistory(2); len(got) != 0 {
		t.Fatalf("a stopped game's use was kept: %+v", got)
	}
	// Old samples age out, and at most historyKept are kept.
	h.history.games[1] = nil
	for i := 0; i < historyKept+10; i++ {
		h.history.games[1] = append(h.history.games[1], Sample{T: 0})
	}
	h.history.games[1][len(h.history.games[1])-1].T = 1 << 40
	if got := h.GameHistory(1); len(got) != 1 {
		t.Fatalf("old samples kept: %d", len(got))
	}
}
