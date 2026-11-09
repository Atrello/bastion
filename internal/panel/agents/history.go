package agents

import (
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// A game's use over the last ten minutes, from the metrics its agent sends every 10 seconds, for the
// charts on its page. Kept in memory: a restarted panel starts again.

const (
	historyKept  = 60 // ten minutes of samples
	historyEvery = 5 * time.Second
)

// Sample is a game's use at one moment. NetIn and NetOut are totals since the game started; the web app
// works out the rate between samples.
type Sample struct {
	T      int64   `json:"t"` // Unix seconds
	CPU    float64 `json:"cpu"`
	Mem    uint64  `json:"mem"`
	NetIn  uint64  `json:"netIn"`
	NetOut uint64  `json:"netOut"`
}

type history struct {
	mu    sync.Mutex
	games map[int64][]Sample
}

// record adds each running game's use, at most once every few seconds (metrics also come straight after
// a game is started or stopped).
func (h *history) record(m *protocol.Metrics) {
	now := time.Now().Unix()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.games == nil {
		h.games = map[int64][]Sample{}
	}
	for _, g := range m.Games {
		if g.State != "running" && g.State != "starting" {
			continue
		}
		list := h.games[g.ID]
		if n := len(list); n > 0 && now-list[n-1].T < int64(historyEvery/time.Second) {
			continue
		}
		list = append(list, Sample{T: now, CPU: g.CPUPercent, Mem: g.MemUsed, NetIn: g.NetIn, NetOut: g.NetOut})
		if len(list) > historyKept {
			list = list[len(list)-historyKept:]
		}
		h.games[g.ID] = list
	}
}

// GameHistory is a game's samples from the last ten minutes, oldest first.
func (h *Hub) GameHistory(gameID int64) []Sample {
	h.history.mu.Lock()
	defer h.history.mu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute).Unix()
	out := []Sample{}
	for _, s := range h.history.games[gameID] {
		if s.T >= cutoff {
			out = append(out, s)
		}
	}
	return out
}
