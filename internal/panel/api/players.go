package api

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Who's playing: every half a minute each running game is asked, through its agent, in the game's own
// query protocol (which its egg decides: eggs.Egg.Query). A game that can't be asked, or doesn't answer,
// has no player count.

type players struct {
	Online int      `json:"online"`
	Max    int      `json:"max"`
	Names  []string `json:"names"`
	asked  time.Time
}

// playersOf is who was playing a game when it was last asked, if that was lately.
func (api *API) playersOf(gameID int64) *players {
	api.gameEvents.mu.Lock()
	defer api.gameEvents.mu.Unlock()
	p, ok := api.gameEvents.players[gameID]
	if !ok || time.Since(p.asked) > 2*time.Minute {
		return nil
	}
	if p.Names == nil {
		p.Names = []string{}
	}
	return &p
}

// askPlayers asks every running game (states is each game's state now) who's playing, all at once.
func (api *API) askPlayers(ctx context.Context, states map[int64]string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for id, state := range states {
		if state != "running" {
			api.gameEvents.mu.Lock()
			delete(api.gameEvents.players, id)
			api.gameEvents.mu.Unlock()
			continue
		}
		g, err := api.loadGame(ctx, id)
		if err != nil {
			continue
		}
		e, err := api.loadEgg(ctx, g.EggID)
		if err != nil || e.Query() == "" {
			continue
		}
		// The game's own port first; Steam games often answer on a query port they name, or the next one up.
		ports := []int{g.Ports[0]}
		if e.Query() == "source" {
			for _, v := range e.Variables {
				if v.Env != "QUERY_PORT" {
					continue
				}
				value := v.Default
				if set, ok := g.Variables[v.Env]; ok {
					value = set
				}
				if port, err := strconv.Atoi(value); err == nil && port > 0 && port < 65536 {
					ports = append([]int{port}, ports...)
				}
			}
			ports = append(ports, g.Ports[0]+1)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res protocol.GameQueryResult
			if api.hub.Request(ctx, g.ServerID, protocol.OpGameQuery, protocol.GameQueryArgs{ID: g.ID, Kind: e.Query(), Ports: ports}, &res) != nil {
				return // it didn't answer (still starting, or an agent from before this): no count
			}
			api.gameEvents.mu.Lock()
			api.gameEvents.players[g.ID] = players{Online: res.Online, Max: res.Max, Names: res.Names, asked: time.Now()}
			api.gameEvents.mu.Unlock()
		}()
	}
	wg.Wait()
}
