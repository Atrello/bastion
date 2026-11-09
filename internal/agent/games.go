package agent

// Game servers. Each game runs from its egg (Pterodactyl's game definitions) in its own container,
// bastion-game-<id>, with its files in the games folder (/var/lib/bastion/games/<id>) mounted at
// /home/container. The panel works out everything from the egg; the agent checks what it's given, installs,
// runs, stops and reports on each one.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Games run as this user, as Pterodactyl's do, never as root. Their files are owned by it.
const (
	gameUID = 988
	gameGID = 988
)

func gamesDir() string {
	if d := os.Getenv("BASTION_AGENT_GAMES_DIR"); d != "" {
		return d
	}
	return "/var/lib/bastion/games"
}

func gameDir(id int64) string       { return filepath.Join(gamesDir(), strconv.FormatInt(id, 10)) }
func gameContainer(id int64) string { return "bastion-game-" + strconv.FormatInt(id, 10) }
func installLogPath(id int64) string {
	return filepath.Join(gamesDir(), strconv.FormatInt(id, 10)+".install.log")
}
func installContainer(id int64) string { return "bastion-install-" + strconv.FormatInt(id, 10) }

var (
	imageRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]{0,254}$`)
	envName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	// Pterodactyl's installers name their shell: bash, ash, sh or a path to one.
	entrypoint = regexp.MustCompile(`^[a-z/]{1,40}$`)
)

// checkSpec refuses anything that isn't plainly a game server's settings.
func checkSpec(s protocol.GameSpec) error {
	switch {
	case s.ID <= 0:
		return errors.New("that isn't a game server")
	case !imageRef.MatchString(s.Image):
		return errors.New("the egg's image isn't an image name")
	case s.MemoryMB < 128 || s.MemoryMB > 1<<20:
		return errors.New("memory should be between 128 MB and 1 TB")
	case s.CPUPercent < 0 || s.CPUPercent > 6400:
		return errors.New("CPU should be between 0 (no limit) and 6400%")
	case len(s.Ports) == 0 || len(s.Ports) > 20:
		return errors.New("a game server needs a port (and at most 20)")
	case strings.ContainsRune(s.Startup, 0) || len(s.Startup) > 8<<10:
		return errors.New("the startup command can't be used")
	}
	for _, p := range s.Ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%d isn't a port", p)
		}
	}
	for k, v := range s.Env {
		if !envName.MatchString(k) || strings.ContainsRune(v, 0) || len(v) > 8<<10 {
			return fmt.Errorf("the variable %q can't be used", k)
		}
	}
	if s.InstallScript != "" && (!imageRef.MatchString(s.InstallImage) || !entrypoint.MatchString(s.InstallEntrypoint)) {
		return errors.New("the egg's installer can't be used")
	}
	return nil
}

// gameEnv is the environment a game and its installer get: the egg's variables, then what Pterodactyl's
// images expect to find.
func gameEnv(s protocol.GameSpec) []string {
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys)+5)
	for _, k := range keys {
		env = append(env, "-e", k+"="+s.Env[k])
	}
	return append(env,
		"-e", "SERVER_MEMORY="+strconv.Itoa(s.MemoryMB),
		"-e", "SERVER_IP=0.0.0.0",
		"-e", "SERVER_PORT="+strconv.Itoa(s.Ports[0]),
		"-e", "P_SERVER_UUID="+gameContainer(s.ID),
	)
}

// What the games are doing that Docker can't say: installing, an install that failed, and which stops
// were asked for (so a game that was stopped isn't reported as crashed).
type gameTracker struct {
	mu         sync.Mutex
	installing map[int64]bool
	failed     map[int64]bool
	stopped    map[int64]bool
	consoles   map[int64]*gameConsole
	disk       map[int64]uint64
}

var games = &gameTracker{
	installing: map[int64]bool{}, failed: map[int64]bool{}, stopped: map[int64]bool{},
	consoles: map[int64]*gameConsole{}, disk: map[int64]uint64{},
}

// installGame runs the egg's installer in the background, into the game's folder. Big games (a Steam
// download) take a while, so it has two hours; its output goes to the install log the console shows.
func installGame(s protocol.GameSpec) error {
	if err := checkSpec(s); err != nil {
		return err
	}
	games.mu.Lock()
	defer games.mu.Unlock()
	if games.installing[s.ID] {
		return errors.New("it's already installing")
	}
	if running, _ := gameRunning(context.Background(), s.ID); running {
		return errors.New("stop it before reinstalling")
	}
	games.installing[s.ID] = true
	delete(games.failed, s.ID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		err := runInstall(ctx, s)
		games.mu.Lock()
		delete(games.installing, s.ID)
		games.failed[s.ID] = err != nil
		games.mu.Unlock()
	}()
	return nil
}

func runInstall(ctx context.Context, s protocol.GameSpec) error {
	dir := gameDir(s.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	log, err := os.Create(installLogPath(s.ID))
	if err != nil {
		return err
	}
	defer log.Close()
	if s.InstallScript != "" {
		scripts, err := os.MkdirTemp(gamesDir(), ".install-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(scripts)
		script := strings.ReplaceAll(s.InstallScript, "\r\n", "\n")
		if err := os.WriteFile(filepath.Join(scripts, "install.sh"), []byte(script), 0o755); err != nil {
			return err
		}
		fmt.Fprintf(log, "Installing with %s…\n", s.InstallImage)
		_, _ = docker(ctx, "", "rm", "-f", installContainer(s.ID))
		args := append([]string{"run", "--rm", "--name", installContainer(s.ID),
			"-v", dir + ":/mnt/server", "-v", scripts + ":/mnt/install:ro",
			"--memory", strconv.Itoa(max(s.MemoryMB, 1024)) + "m", "--pids-limit", "1024",
			"--security-opt", "no-new-privileges"},
			gameEnv(s)...)
		args = append(args, "--entrypoint", s.InstallEntrypoint, s.InstallImage, "/mnt/install/install.sh")
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(log, "\nThe installer failed (%v). Its output is above.\n", err)
			return err
		}
	}
	if err := ownAll(dir); err != nil {
		return err
	}
	fmt.Fprintln(log, "\nInstalled.")
	return nil
}

// ownAll gives a game's files to the user it runs as (installers run as root).
func ownAll(dir string) error {
	return filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, gameUID, gameGID)
	})
}

// startGame makes the game's container afresh from its spec (so changed settings apply) and starts it.
func startGame(ctx context.Context, s protocol.GameSpec) error {
	if err := checkSpec(s); err != nil {
		return err
	}
	games.mu.Lock()
	installing := games.installing[s.ID]
	games.mu.Unlock()
	if installing {
		return errors.New("it's still installing")
	}
	if running, _ := gameRunning(ctx, s.ID); running {
		return errors.New("it's already running")
	}
	if _, err := os.Stat(gameDir(s.ID)); err != nil {
		return errors.New("it isn't installed on this server")
	}
	if err := applyConfigFiles(s.ID, s.ConfigFiles); err != nil {
		return fmt.Errorf("filling in its config files: %w", err)
	}
	_, _ = docker(ctx, "", "rm", "-f", gameContainer(s.ID))
	memory := strconv.Itoa(s.MemoryMB) + "m"
	args := []string{"run", "-d", "-i", "--name", gameContainer(s.ID), "--label", "bastion.game=" + strconv.FormatInt(s.ID, 10),
		"--user", fmt.Sprintf("%d:%d", gameUID, gameGID), "--workdir", "/home/container",
		"-v", gameDir(s.ID) + ":/home/container",
		"--memory", memory, "--memory-swap", memory, "--pids-limit", "1024",
		"--security-opt", "no-new-privileges",
		// What Pterodactyl's Wings takes away from games too: nothing a game server needs.
		"--cap-drop", "setpcap", "--cap-drop", "mknod", "--cap-drop", "audit_write", "--cap-drop", "net_raw",
		"--cap-drop", "dac_override", "--cap-drop", "fowner", "--cap-drop", "fsetid", "--cap-drop", "net_bind_service",
		"--cap-drop", "sys_chroot", "--cap-drop", "setfcap",
		"--log-driver", "json-file", "--log-opt", "max-size=20m", "--log-opt", "max-file=2",
		"-e", "STARTUP=" + s.Startup, "-e", "HOME=/home/container"}
	if s.CPUPercent > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(float64(s.CPUPercent)/100, 'f', 2, 64))
	}
	args = append(args, gameEnv(s)...)
	for _, p := range s.Ports {
		port := strconv.Itoa(p)
		args = append(args, "-p", port+":"+port+"/tcp", "-p", port+":"+port+"/udp")
	}
	args = append(args, s.Image)
	if _, err := docker(ctx, "", args...); err != nil {
		return err
	}
	games.mu.Lock()
	delete(games.stopped, s.ID)
	if c := games.consoles[s.ID]; c != nil {
		c.close() // the old container's console
		delete(games.consoles, s.ID)
	}
	games.mu.Unlock()
	return nil
}

func gameRunning(ctx context.Context, id int64) (bool, error) {
	out, err := docker(ctx, "", "inspect", "--format", "{{.State.Running}}", gameContainer(id))
	return out == "true", err
}

// powerGame stops, kills or types a command into a game.
func powerGame(ctx context.Context, a protocol.GamePowerArgs) error {
	if a.ID <= 0 {
		return errors.New("that isn't a game server")
	}
	switch a.Action {
	case "command":
		if strings.ContainsAny(a.Command, "\r\n\x00") || len(a.Command) > 4096 {
			return errors.New("a command is one line")
		}
		return sendCommand(a.ID, a.Command)
	case "kill":
		games.mu.Lock()
		games.stopped[a.ID] = true
		games.mu.Unlock()
		_, err := docker(ctx, "", "kill", gameContainer(a.ID))
		return err
	case "stop":
		games.mu.Lock()
		games.stopped[a.ID] = true
		games.mu.Unlock()
		// The egg's way first ("stop", or ^C for Ctrl+C), then Docker's if it hasn't stopped in 30 seconds.
		var err error
		switch {
		case a.StopWith == "^C" || a.StopWith == "":
			_, err = docker(ctx, "", "kill", "--signal", "SIGINT", gameContainer(a.ID))
		case strings.HasPrefix(a.StopWith, "^"):
			_, err = docker(ctx, "", "kill", "--signal", "SIG"+strings.TrimPrefix(strings.ToUpper(a.StopWith[1:]), "SIG"), gameContainer(a.ID))
		default:
			err = sendCommand(a.ID, a.StopWith)
		}
		if err != nil {
			_, err = docker(ctx, "", "stop", "--time", "10", gameContainer(a.ID))
			return err
		}
		wait, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := docker(wait, "", "wait", gameContainer(a.ID)); err != nil {
			_, err = docker(ctx, "", "stop", "--time", "10", gameContainer(a.ID))
			return err
		}
		return nil
	}
	return errors.New("unknown power action " + a.Action)
}

// gameConsole is a `docker attach` to a game, kept open to type commands into it. Its output is read
// from the container's logs instead, so this only writes.
type gameConsole struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
}

func (c *gameConsole) close() {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *gameConsole) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func sendCommand(id int64, line string) error {
	games.mu.Lock()
	defer games.mu.Unlock()
	c := games.consoles[id]
	if c == nil || !c.alive() {
		cmd := exec.Command("docker", "attach", "--sig-proxy=false", gameContainer(id))
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			return err
		}
		c = &gameConsole{cmd: cmd, stdin: stdin, done: make(chan struct{})}
		go func() { _ = cmd.Wait(); close(c.done) }()
		games.consoles[id] = c
		time.Sleep(200 * time.Millisecond) // let attach connect before the first line
		if !c.alive() {
			return errors.New("it isn't running")
		}
	}
	_, err := io.WriteString(c.stdin, line+"\n")
	return err
}

// deleteGame removes a game's container, files, backups and install log.
func deleteGame(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("that isn't a game server")
	}
	_, _ = docker(ctx, "", "rm", "-f", gameContainer(id))
	games.mu.Lock()
	if c := games.consoles[id]; c != nil {
		c.close()
	}
	delete(games.consoles, id)
	delete(games.disk, id)
	games.mu.Unlock()
	_ = os.Remove(installLogPath(id))
	if err := os.RemoveAll(backupDir(id)); err != nil {
		return err
	}
	return os.RemoveAll(gameDir(id))
}

// gameStatuses is every game on this server, for the metrics sent every 10 seconds.
func gameStatuses(ctx context.Context) []protocol.GameStatus {
	entries, err := os.ReadDir(gamesDir())
	if err != nil {
		return nil
	}
	byID := map[int64]*protocol.GameStatus{}
	var ids []int64
	for _, e := range entries {
		if id, err := strconv.ParseInt(e.Name(), 10, 64); err == nil && e.IsDir() && id > 0 {
			byID[id] = &protocol.GameStatus{ID: id, State: "stopped"}
			ids = append(ids, id)
		}
	}
	games.mu.Lock()
	for id := range games.installing {
		if byID[id] == nil {
			byID[id] = &protocol.GameStatus{ID: id}
			ids = append(ids, id)
		}
	}
	games.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	// Docker's view: running or not, how it exited, and since when.
	out, _ := docker(ctx, "", "ps", "--all", "--filter", "label=bastion.game", "--format", "{{.Names}}")
	var running []string
	if names := strings.Fields(out); len(names) > 0 {
		info, _ := docker(ctx, "", append([]string{"inspect", "--format", "{{.Name}} {{.State.Running}} {{.State.ExitCode}} {{.State.StartedAt}}"}, names...)...)
		for _, line := range strings.Split(info, "\n") {
			f := strings.Fields(line)
			if len(f) != 4 {
				continue
			}
			id, _ := strconv.ParseInt(strings.TrimPrefix(f[0], "/bastion-game-"), 10, 64)
			g := byID[id]
			if g == nil {
				continue
			}
			games.mu.Lock()
			stopped := games.stopped[id]
			games.mu.Unlock()
			switch {
			case f[1] == "true":
				g.State, g.StartedAt = "running", f[3]
				running = append(running, gameContainer(id))
			case f[2] != "0" && !stopped:
				g.State = "crashed"
			}
		}
	}
	if len(running) > 0 {
		stats, _ := docker(ctx, "", append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, running...)...)
		for _, line := range strings.Split(stats, "\n") {
			var st struct{ Name, CPUPerc, MemUsage string }
			if json.Unmarshal([]byte(line), &st) != nil {
				continue
			}
			id, _ := strconv.ParseInt(strings.TrimPrefix(st.Name, "bastion-game-"), 10, 64)
			if g := byID[id]; g != nil {
				g.CPUPercent, _ = strconv.ParseFloat(strings.TrimSuffix(st.CPUPerc, "%"), 64)
				used, _, _ := strings.Cut(st.MemUsage, " / ")
				g.MemUsed = parseSize(used)
			}
		}
	}

	games.mu.Lock()
	defer games.mu.Unlock()
	list := make([]protocol.GameStatus, 0, len(ids))
	for _, id := range ids {
		g := byID[id]
		if games.installing[id] {
			g.State = "installing"
		} else if games.failed[id] {
			g.State = "install-failed"
		}
		g.DiskUsed = games.disk[id]
		list = append(list, *g)
	}
	return list
}

// measureGames adds up each game's folder every few minutes: walking a big world isn't free.
func measureGames(ctx context.Context) {
	for {
		entries, _ := os.ReadDir(gamesDir())
		for _, e := range entries {
			id, err := strconv.ParseInt(e.Name(), 10, 64)
			if err != nil || !e.IsDir() {
				continue
			}
			var total uint64
			_ = filepath.WalkDir(filepath.Join(gamesDir(), e.Name()), func(_ string, d fs.DirEntry, err error) error {
				if err == nil && d.Type().IsRegular() {
					if info, err := d.Info(); err == nil {
						total += uint64(info.Size())
					}
				}
				return nil
			})
			games.mu.Lock()
			games.disk[id] = total
			games.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Minute):
		}
	}
}

// installLog is the end of a game's install log, for its console while it installs.
func installLog(id int64) ([]byte, error) {
	f, err := os.Open(installLogPath(id))
	if err != nil {
		return nil, errors.New("no install log")
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > 64<<10 {
		_, _ = f.Seek(-64<<10, io.SeekEnd)
	}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}
