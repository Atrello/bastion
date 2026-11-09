package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/eggs"
	"github.com/Atrello/bastion/internal/protocol"
)

// Game servers: made from an egg on one of the servers, then run, stopped, configured and shared from
// here. The owner sees them all; a friend sees only the ones shared with them, and can't change their
// limits, delete them or share them on.

type game struct {
	ID          int64
	Name        string
	EggID       int64
	EggName     string
	ServerID    int64
	Image       string
	MemoryMB    int
	CPUPercent  int
	DiskMB      int
	Ports       []int
	Variables   map[string]string
	WarnCommand string
}

// gameState is what the panel is doing to games that their agents can't say: a stop counting down.
type gameState struct {
	mu       sync.Mutex
	stopping map[int64]bool
}

func (api *API) gameRoutes() {
	api.games = &gameState{stopping: map[int64]bool{}}
	m := api.mux
	m.Handle("GET /api/games", api.signedIn(api.listGames))
	m.Handle("POST /api/games", api.owner(api.createGame))
	m.Handle("GET /api/games/{id}", api.gameUser(api.getGame))
	m.Handle("PUT /api/games/{id}", api.gameOwner(api.updateGame))
	m.Handle("DELETE /api/games/{id}", api.gameOwner(api.deleteGame))
	m.Handle("POST /api/games/{id}/reinstall", api.gameOwner(api.reinstallGame))
	m.Handle("POST /api/games/{id}/power/{action}", api.gameUser(api.powerGame))
	m.Handle("GET /api/games/{id}/console", api.gameUser(api.gameConsole))
	m.Handle("POST /api/games/{id}/command", api.gameUser(api.gameCommand))
	m.Handle("GET /api/streams/game", api.signedIn(api.gameStream))
	m.Handle("GET /api/games/{id}/startup", api.gameUser(api.gameStartup))
	m.Handle("PUT /api/games/{id}/startup", api.gameUser(api.saveGameStartup))
	m.Handle("GET /api/games/{id}/shares", api.gameOwner(api.gameShares))
	m.Handle("POST /api/games/{id}/shares", api.gameOwner(api.inviteFriend))
	m.Handle("DELETE /api/games/{id}/shares/{user}", api.gameOwner(api.unshareGame))
	api.gameFileRoutes()
	api.scheduleRoutes()
}

const gameColumns = `g.id, g.name, g.egg_id, e.name, g.server_id, g.image, g.memory_mb, g.cpu_percent, g.disk_mb, g.ports, g.variables, g.warn_command`

func scanGame(row interface{ Scan(...any) error }) (*game, error) {
	var g game
	var ports, vars string
	if err := row.Scan(&g.ID, &g.Name, &g.EggID, &g.EggName, &g.ServerID, &g.Image, &g.MemoryMB, &g.CPUPercent, &g.DiskMB, &ports, &vars, &g.WarnCommand); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(ports), &g.Ports)
	_ = json.Unmarshal([]byte(vars), &g.Variables)
	if g.Variables == nil {
		g.Variables = map[string]string{}
	}
	return &g, nil
}

func (api *API) loadGame(ctx context.Context, id int64) (*game, error) {
	return scanGame(api.db.QueryRowContext(ctx, `SELECT `+gameColumns+` FROM games g JOIN eggs e ON e.id = g.egg_id WHERE g.id = ?`, id))
}

// canSee says whether a user may use a game: the owner, or a friend it's shared with.
func (api *API) canSee(ctx context.Context, u *auth.User, gameID int64) bool {
	if u.Role == auth.RoleOwner {
		return true
	}
	var n int
	_ = api.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_shares WHERE game_id = ? AND user_id = ?`, gameID, u.ID).Scan(&n)
	return n > 0
}

// gameUser lets through the owner and friends the game is shared with, handing over the game. Anyone else
// is told there's no such game, so they can't tell which exist.
func (api *API) gameUser(next func(http.ResponseWriter, *http.Request, *auth.User, *game)) http.Handler {
	return api.signedIn(func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		g, err := api.loadGame(r.Context(), id)
		if err != nil || !api.canSee(r.Context(), u, id) {
			writeError(w, http.StatusNotFound, "no such game server")
			return
		}
		next(w, r, u, g)
	})
}

func (api *API) gameOwner(next func(http.ResponseWriter, *http.Request, *auth.User, *game)) http.Handler {
	return api.gameUser(func(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
		if u.Role != auth.RoleOwner {
			writeError(w, http.StatusForbidden, "only the panel's owner can do that")
			return
		}
		next(w, r, u, g)
	})
}

// spec works out everything the agent needs to install or run a game from its egg and settings.
func (api *API) spec(ctx context.Context, g *game) (protocol.GameSpec, *eggs.Egg, error) {
	e, err := api.loadEgg(ctx, g.EggID)
	if err != nil {
		return protocol.GameSpec{}, nil, err
	}
	env := map[string]string{}
	for _, v := range e.Variables {
		env[v.Env] = v.Default
		if value, set := g.Variables[v.Env]; set {
			env[v.Env] = value
		}
	}
	fill := eggs.Placeholders{Port: g.Ports[0], MemoryMB: g.MemoryMB, Env: env}
	files, _ := e.ConfigFiles()
	configFiles := make([]protocol.ConfigFile, 0, len(files))
	for _, f := range files {
		find := map[string]string{}
		for k, v := range f.Find {
			find[k] = fill.Fill(v)
		}
		configFiles = append(configFiles, protocol.ConfigFile{File: f.File, Parser: f.Parser, Find: find})
	}
	install := e.Scripts.Installation
	return protocol.GameSpec{
		ID: g.ID, Image: g.Image, Startup: fill.Fill(e.Startup), StopWith: e.Config.Stop, DoneText: e.DoneText(), Env: env,
		MemoryMB: g.MemoryMB, CPUPercent: g.CPUPercent, DiskMB: g.DiskMB, Ports: g.Ports, ConfigFiles: configFiles,
		InstallImage: install.Container, InstallEntrypoint: install.Entrypoint, InstallScript: install.Script,
	}, e, nil
}

// gameView is a game server as the web app shows it (web/src/lib/types.ts, GameServer).
type gameView struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Egg         string    `json:"egg"`
	EggID       int64     `json:"eggId"`
	ServerID    int64     `json:"serverId"`
	ServerName  string    `json:"serverName"`
	State       string    `json:"state"` // starting until its console says it has started, then running
	Address     string    `json:"address"`
	Players     *struct{} `json:"players"`
	Image       string    `json:"image"`
	Ports       []int     `json:"ports"`
	CPUPercent  float64   `json:"cpuPercent"`
	CPULimit    int       `json:"cpuLimit"`
	MemUsed     uint64    `json:"memUsed"`
	MemLimit    uint64    `json:"memLimit"`
	DiskUsed    uint64    `json:"diskUsed"`
	DiskLimit   uint64    `json:"diskLimit"`
	UptimeSecs  int64     `json:"uptimeSecs"`
	WarnCommand string    `json:"warnCommand"`
	Owner       bool      `json:"owner"` // the viewer can change its limits, delete and share it
}

func (api *API) view(g *game, serverName string, owner bool) gameView {
	v := gameView{ID: g.ID, Name: g.Name, Egg: g.EggName, EggID: g.EggID, ServerID: g.ServerID, ServerName: serverName,
		State: "offline", Image: g.Image, Ports: g.Ports, CPULimit: g.CPUPercent, WarnCommand: g.WarnCommand, Owner: owner,
		MemLimit: uint64(g.MemoryMB) << 20, DiskLimit: uint64(g.DiskMB) << 20}
	live := api.hub.Live(g.ServerID)
	host := serverName
	if live.Hello != nil && live.Hello.Address != "" {
		host = live.Hello.Address
	}
	v.Address = host + ":" + strconv.Itoa(g.Ports[0])
	if !live.Online {
		return v
	}
	v.State = "stopped"
	if live.Metrics != nil {
		for _, s := range live.Metrics.Games {
			if s.ID != g.ID {
				continue
			}
			v.State, v.CPUPercent, v.MemUsed, v.DiskUsed = s.State, s.CPUPercent, s.MemUsed, s.DiskUsed
			if started, err := time.Parse(time.RFC3339Nano, s.StartedAt); err == nil && (s.State == "running" || s.State == "starting") {
				v.UptimeSecs = int64(time.Since(started).Seconds())
			}
		}
	}
	api.games.mu.Lock()
	if api.games.stopping[g.ID] && (v.State == "running" || v.State == "starting") {
		v.State = "stopping"
	}
	api.games.mu.Unlock()
	return v
}

func (api *API) serverName(ctx context.Context, id int64) string {
	var name string
	_ = api.db.QueryRowContext(ctx, `SELECT name FROM servers WHERE id = ?`, id).Scan(&name)
	return name
}

func (api *API) listGames(w http.ResponseWriter, r *http.Request, u *auth.User) {
	query := `SELECT ` + gameColumns + `, s.name FROM games g JOIN eggs e ON e.id = g.egg_id JOIN servers s ON s.id = g.server_id`
	var args []any
	if u.Role != auth.RoleOwner {
		query += ` WHERE g.id IN (SELECT game_id FROM game_shares WHERE user_id = ?)`
		args = append(args, u.ID)
	}
	rows, err := api.db.QueryContext(r.Context(), query+` ORDER BY g.name`, args...)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []gameView{}
	for rows.Next() {
		var serverName string
		g, err := scanGame(scanWithExtra{rows, &serverName})
		if err == nil {
			list = append(list, api.view(g, serverName, u.Role == auth.RoleOwner))
		}
	}
	writeJSON(w, list)
}

// scanWithExtra scans a game's columns and one more after them.
type scanWithExtra struct {
	rows  *sql.Rows
	extra any
}

func (s scanWithExtra) Scan(dest ...any) error { return s.rows.Scan(append(dest, s.extra)...) }

func (api *API) getGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	writeJSON(w, api.view(g, api.serverName(r.Context(), g.ServerID), u.Role == auth.RoleOwner))
}

type gameSettings struct {
	Name        string `json:"name"`
	EggID       int64  `json:"eggId"`
	ServerID    int64  `json:"serverId"`
	Image       string `json:"image"`
	MemoryMB    int    `json:"memoryMb"`
	CPUPercent  int    `json:"cpuPercent"`
	DiskMB      int    `json:"diskMb"`
	Ports       []int  `json:"ports"`
	WarnCommand string `json:"warnCommand"`
}

// check checks a game's settings, filling in what can be: the egg's first image, and a warning command for
// games that have one ("say" for Minecraft-like games).
func (api *API) checkSettings(ctx context.Context, s *gameSettings, gameID int64, e *eggs.Egg) error {
	s.Name = strings.TrimSpace(s.Name)
	switch {
	case s.Name == "" || len(s.Name) > 60:
		return errors.New("give it a name, up to 60 characters")
	case s.MemoryMB < 256 || s.MemoryMB > 1<<20:
		return errors.New("memory should be at least 256 MB")
	case s.CPUPercent < 0 || s.CPUPercent > 6400:
		return errors.New("CPU should be from 0 (no limit) to 6400%")
	case s.DiskMB < 0:
		return errors.New("disk should be 0 (no limit) or more")
	case len(s.Ports) == 0 || len(s.Ports) > 20:
		return errors.New("it needs a port")
	case strings.Count(s.WarnCommand, "%s") > 1 || strings.ContainsAny(s.WarnCommand, "\r\n") || len(s.WarnCommand) > 200:
		return errors.New("the warning command is one line, with %s where the message goes")
	}
	if s.Image == "" && len(e.Images) > 0 {
		s.Image = e.Images[0].Ref
	}
	if !e.HasImage(s.Image) {
		return errors.New("that image isn't one of the egg's")
	}
	seen := map[int]bool{}
	for _, p := range s.Ports {
		if p < 1024 || p > 65535 || seen[p] {
			return fmt.Errorf("%d can't be used: ports are 1024 to 65535, each once", p)
		}
		seen[p] = true
	}
	// No two games on a server can have the same port.
	rows, err := api.db.QueryContext(ctx, `SELECT name, ports FROM games WHERE server_id = ? AND id != ?`, s.ServerID, gameID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, raw string
		var ports []int
		if rows.Scan(&name, &raw) == nil && json.Unmarshal([]byte(raw), &ports) == nil {
			for _, p := range ports {
				if seen[p] {
					return fmt.Errorf("port %d is %s's on that server", p, name)
				}
			}
		}
	}
	return nil
}

// createGame saves a new game server and starts installing it.
func (api *API) createGame(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var s gameSettings
	if !readJSON(w, r, &s) {
		return
	}
	e, err := api.loadEgg(r.Context(), s.EggID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if api.serverName(r.Context(), s.ServerID) == "" {
		writeError(w, http.StatusBadRequest, "no such server")
		return
	}
	if s.WarnCommand == "" && strings.Contains(e.Startup, "java") {
		s.WarnCommand = "say %s"
	}
	if err := api.checkSettings(r.Context(), &s, 0, e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ports, _ := json.Marshal(s.Ports)
	res, err := api.db.ExecContext(r.Context(), `INSERT INTO games (name, egg_id, server_id, image, memory_mb, cpu_percent, disk_mb, ports, warn_command)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.Name, s.EggID, s.ServerID, s.Image, s.MemoryMB, s.CPUPercent, s.DiskMB, string(ports), s.WarnCommand)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	g, err := api.loadGame(r.Context(), id)
	if err == nil {
		err = api.install(r.Context(), g)
	}
	if err != nil {
		// Not installed: it's not kept, so it can be tried again.
		_, _ = api.db.ExecContext(r.Context(), `DELETE FROM games WHERE id = ?`, id)
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game server made", "user", u.Username, "game", s.Name, "egg", e.Name)
	writeJSON(w, api.view(g, api.serverName(r.Context(), g.ServerID), true))
}

func (api *API) install(ctx context.Context, g *game) error {
	spec, _, err := api.spec(ctx, g)
	if err != nil {
		return err
	}
	return api.hub.Request(ctx, g.ServerID, protocol.OpGameInstall, spec, nil)
}

// updateGame changes a game's name, limits, image, ports and warning. They apply from its next start.
func (api *API) updateGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var s gameSettings
	if !readJSON(w, r, &s) {
		return
	}
	s.ServerID = g.ServerID
	e, err := api.loadEgg(r.Context(), g.EggID)
	if err == nil {
		err = api.checkSettings(r.Context(), &s, g.ID, e)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ports, _ := json.Marshal(s.Ports)
	if _, err := api.db.ExecContext(r.Context(), `UPDATE games SET name = ?, image = ?, memory_mb = ?, cpu_percent = ?, disk_mb = ?, ports = ?, warn_command = ? WHERE id = ?`,
		s.Name, s.Image, s.MemoryMB, s.CPUPercent, s.DiskMB, string(ports), s.WarnCommand, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game server changed", "user", u.Username, "game", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameDelete, protocol.GameRef{ID: g.ID}, nil); err != nil {
		api.agentFail(w, r, err)
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM games WHERE id = ?`, g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game server deleted", "user", u.Username, "game", g.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) reinstallGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	if err := api.install(r.Context(), g); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game server reinstalled", "user", u.Username, "game", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

// power starts, stops, restarts or kills a game. A stop or restart with a warning tells the players
// first, then waits before stopping, in the background.
func (api *API) power(ctx context.Context, g *game, action string, warnSecs int) error {
	spec, _, err := api.spec(ctx, g)
	if err != nil {
		return err
	}
	stop := func(ctx context.Context) error {
		return api.hub.Request(ctx, g.ServerID, protocol.OpGamePower, protocol.GamePowerArgs{ID: g.ID, Action: "stop", StopWith: spec.StopWith}, nil)
	}
	switch action {
	case "start":
		if over := api.overDisk(g); over != "" {
			return refusal(over)
		}
		return api.hub.Request(ctx, g.ServerID, protocol.OpGameStart, spec, nil)
	case "kill":
		return api.hub.Request(ctx, g.ServerID, protocol.OpGamePower, protocol.GamePowerArgs{ID: g.ID, Action: "kill"}, nil)
	case "stop", "restart":
		if warnSecs > 0 && g.WarnCommand != "" {
			verb := map[string]string{"stop": "stopping", "restart": "restarting"}[action]
			message := fmt.Sprintf("Server %s in %d seconds", verb, warnSecs)
			command := strings.Replace(g.WarnCommand, "%s", message, 1)
			if err := api.hub.Request(ctx, g.ServerID, protocol.OpGamePower, protocol.GamePowerArgs{ID: g.ID, Action: "command", Command: command}, nil); err != nil {
				return err
			}
			api.games.mu.Lock()
			api.games.stopping[g.ID] = true
			api.games.mu.Unlock()
			go func() {
				defer func() {
					api.games.mu.Lock()
					delete(api.games.stopping, g.ID)
					api.games.mu.Unlock()
				}()
				time.Sleep(time.Duration(warnSecs) * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				err := stop(ctx)
				if err == nil && action == "restart" {
					err = api.hub.Request(ctx, g.ServerID, protocol.OpGameStart, spec, nil)
				}
				if err != nil {
					api.log.Warn("game server stop failed", "game", g.ID, "err", err)
				}
			}()
			return nil
		}
		if err := stop(ctx); err != nil || action == "stop" {
			return err
		}
		return api.hub.Request(ctx, g.ServerID, protocol.OpGameStart, spec, nil)
	}
	return errors.New("no such power action")
}

func (api *API) powerGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	action := r.PathValue("action")
	if action != "start" && action != "stop" && action != "restart" && action != "kill" {
		writeError(w, http.StatusNotFound, "no such power action")
		return
	}
	var body struct {
		Warn int `json:"warn"` // seconds to warn players before a stop or restart
	}
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	if err := api.power(r.Context(), g, action, min(max(body.Warn, 0), 600)); err != nil {
		var no refusal
		if errors.As(err, &no) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			api.agentFail(w, r, err)
		}
		return
	}
	api.log.Info("game server power", "user", u.Username, "game", g.ID, "action", action)
	w.WriteHeader(http.StatusNoContent)
}

// refusal is something the panel won't do, said to the person asking.
type refusal string

func (r refusal) Error() string { return string(r) }

// overDisk says why a game can't start or take more files, if it's over its disk limit.
func (api *API) overDisk(g *game) string {
	if g.DiskMB == 0 {
		return ""
	}
	if v := api.view(g, "", false); v.DiskUsed > v.DiskLimit {
		return fmt.Sprintf("it's using more than its %d MB of disk: delete some files (or raise its limit) first", g.DiskMB)
	}
	return ""
}

// gameConsole is the console's recent output: the game's, or the install log while it installs and until
// it first runs.
func (api *API) gameConsole(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	var lines protocol.LogLines
	v := api.view(g, "", false)
	ran := v.State != "installing" && v.State != "install-failed" &&
		api.hub.Request(r.Context(), g.ServerID, protocol.OpStackLogs, protocol.LogsArgs{Container: "bastion-game-" + strconv.FormatInt(g.ID, 10), Tail: 300}, &lines) == nil
	if !ran {
		var res protocol.GameFileResult
		if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGameFiles, protocol.GameFileArgs{ID: g.ID, Op: "install-log"}, &res); err != nil {
			api.agentFail(w, r, err)
			return
		}
		writeJSON(w, map[string]any{"lines": strings.Split(strings.TrimRight(string(res.Data), "\n"), "\n")})
		return
	}
	writeJSON(w, lines)
}

func (api *API) gameCommand(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Command string }
	if !readJSON(w, r, &body) {
		return
	}
	command := strings.TrimSpace(body.Command)
	if command == "" || strings.ContainsAny(command, "\r\n") || len(command) > 1000 {
		writeError(w, http.StatusBadRequest, "a command is one line")
		return
	}
	if err := api.hub.Request(r.Context(), g.ServerID, protocol.OpGamePower, protocol.GamePowerArgs{ID: g.ID, Action: "command", Command: command}, nil); err != nil {
		api.agentFail(w, r, err)
		return
	}
	api.log.Info("game command", "user", u.Username, "game", g.ID, "command", command)
	w.WriteHeader(http.StatusNoContent)
}

// gameStream follows a game's console output live.
func (api *API) gameStream(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	g, err := api.loadGame(r.Context(), id)
	if err != nil || !api.canSee(r.Context(), u, id) {
		writeError(w, http.StatusNotFound, "no such game server")
		return
	}
	api.relay(w, r, g.ServerID, protocol.OpLogsFollow, protocol.LogsArgs{Container: "bastion-game-" + strconv.FormatInt(g.ID, 10), Tail: 0}, false)
}

// startupVariable is one of a game's settings (web/src/lib/types.ts, StartupVariable).
type startupVariable struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Value       string `json:"value"`
	Rules       string `json:"rules"`
	Editable    bool   `json:"editable"`
}

func (api *API) gameStartup(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	spec, e, err := api.spec(r.Context(), g)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	owner := u.Role == auth.RoleOwner
	vars := []startupVariable{}
	for _, v := range e.Variables {
		if owner || v.UserViewable {
			vars = append(vars, startupVariable{Key: v.Env, Name: v.Name, Description: v.Description, Value: spec.Env[v.Env], Rules: v.Rules, Editable: owner || v.UserEditable})
		}
	}
	writeJSON(w, map[string]any{"command": spec.Startup, "image": g.Image, "images": e.Images, "variables": vars})
}

// saveGameStartup checks and saves a game's settings against its egg's rules. Friends can only change
// the ones the egg lets players change.
func (api *API) saveGameStartup(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var values map[string]string
	if !readJSON(w, r, &values) {
		return
	}
	e, err := api.loadEgg(r.Context(), g.EggID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	owner := u.Role == auth.RoleOwner
	for _, v := range e.Variables {
		value, given := values[v.Env]
		if !given {
			continue
		}
		if !owner && !v.UserEditable {
			writeError(w, http.StatusForbidden, v.Name+" can only be changed by the panel's owner")
			return
		}
		if err := v.Check(value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		g.Variables[v.Env] = value
	}
	raw, _ := json.Marshal(g.Variables)
	if _, err := api.db.ExecContext(r.Context(), `UPDATE games SET variables = ? WHERE id = ?`, string(raw), g.ID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game settings changed", "user", u.Username, "game", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) gameShares(w http.ResponseWriter, r *http.Request, _ *auth.User, g *game) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT u.id, u.username FROM game_shares s JOIN users u ON u.id = s.user_id WHERE s.game_id = ? ORDER BY u.username`, g.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	type share struct {
		UserID   int64  `json:"userId"`
		Username string `json:"username"`
	}
	list := []share{}
	for rows.Next() {
		var s share
		if rows.Scan(&s.UserID, &s.Username) == nil {
			list = append(list, s)
		}
	}
	writeJSON(w, list)
}

// inviteFriend makes a one-time link for a friend: they make an account with it, and see only this game.
// An existing friend's username shares it with them instead.
func (api *API) inviteFriend(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	var body struct{ Username string }
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	if name := strings.TrimSpace(body.Username); name != "" {
		res, err := api.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO game_shares (game_id, user_id)
			SELECT ?, id FROM users WHERE username = ? AND role = ?`, g.ID, name, auth.RoleFriend)
		if n, _ := res.RowsAffected(); err != nil || n == 0 {
			writeError(w, http.StatusBadRequest, "there's no friend called "+name+": send them a link instead")
			return
		}
		api.log.Info("game shared", "user", u.Username, "game", g.ID, "with", name)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	token, err := api.auth.NewInvite(r.Context(), g.ID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game invite made", "user", u.Username, "game", g.ID)
	writeJSON(w, map[string]string{"link": api.cfg.PublicURL.String() + "/invite#" + token})
}

func (api *API) unshareGame(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	userID, _ := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM game_shares WHERE game_id = ? AND user_id = ?`, g.ID, userID); err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("game unshared", "user", u.Username, "game", g.ID, "from", userID)
	w.WriteHeader(http.StatusNoContent)
}
