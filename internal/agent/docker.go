package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// What the agent does with Docker, through the docker CLI. Stacks are found by name, among the compose
// projects Docker knows and the folders in the stacks directory, and only their compose file and .env are
// ever read or written, so the panel can't reach other files on the server.

var (
	stackName   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	containerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	// The compose file names Docker looks for, in its order.
	composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
)

const maxFileSize = 1 << 20 // a compose file or .env bigger than 1 MB is a mistake

// docker runs the docker CLI, giving what it printed (and that as the error if it failed).
func docker(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New(text)
	}
	return text, nil
}

func dockerVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := docker(ctx, "", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return ""
	}
	return v
}

// stack is where a stack's files are.
type stack struct {
	name, configFile, dir string
}

// stacks is every compose project Docker knows (running or not), plus folders in the stacks directory
// with a compose file that have never been started.
func stacks(ctx context.Context, stacksDir string) ([]stack, error) {
	out, err := docker(ctx, "", "compose", "ls", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	var found []struct{ Name, ConfigFiles string }
	if out != "" {
		if err := json.Unmarshal([]byte(out), &found); err != nil {
			return nil, fmt.Errorf("docker compose ls: %w", err)
		}
	}
	seen := map[string]bool{}
	var list []stack
	for _, f := range found {
		file := strings.Split(f.ConfigFiles, ",")[0] // the first is the main one
		if !stackName.MatchString(f.Name) || file == "" {
			continue
		}
		list = append(list, stack{name: f.Name, configFile: file, dir: filepath.Dir(file)})
		seen[f.Name] = true
	}
	entries, _ := os.ReadDir(stacksDir)
	for _, e := range entries {
		if !e.IsDir() || seen[e.Name()] || !stackName.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(stacksDir, e.Name())
		if file := composeFileIn(dir); file != "" {
			list = append(list, stack{name: e.Name(), configFile: file, dir: dir})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list, nil
}

func composeFileIn(dir string) string {
	for _, n := range composeNames {
		if info, err := os.Lstat(filepath.Join(dir, n)); err == nil && info.Mode().IsRegular() {
			return filepath.Join(dir, n)
		}
	}
	return ""
}

func findStack(ctx context.Context, stacksDir, name string) (*stack, error) {
	if !stackName.MatchString(name) {
		return nil, errors.New("that isn't a stack name")
	}
	list, err := stacks(ctx, stacksDir)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		if s.name == name {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("there's no stack called %s on this server", name)
}

// readState is everything Docker is running, for the panel.
func readState(ctx context.Context, stacksDir string) (protocol.State, error) {
	containers, err := listContainers(ctx)
	if err != nil {
		return protocol.State{}, err
	}
	list, err := stacks(ctx, stacksDir)
	if err != nil {
		return protocol.State{}, err
	}
	state := protocol.State{Containers: containers, Stacks: make([]protocol.Stack, 0, len(list))}
	for _, s := range list {
		st := protocol.Stack{Name: s.name, ConfigFile: s.configFile, HasEnv: regularFile(filepath.Join(s.dir, ".env"))}
		for _, c := range containers {
			if c.Stack == s.name {
				st.Total++
				if c.State == "running" {
					st.Running++
				}
				if c.Update {
					st.Updates++
				}
			}
		}
		state.Stacks = append(state.Stacks, st)
	}
	return state, nil
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func listContainers(ctx context.Context) ([]protocol.Container, error) {
	out, err := docker(ctx, "", "ps", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	containers := []protocol.Container{}
	for _, line := range strings.Split(out, "\n") {
		var c struct{ ID, Names, Image, State, Status, Ports, Labels string }
		if line == "" || json.Unmarshal([]byte(line), &c) != nil {
			continue
		}
		labels := map[string]string{}
		for _, kv := range strings.Split(c.Labels, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				labels[k] = v
			}
		}
		containers = append(containers, protocol.Container{
			ID: c.ID[:min(12, len(c.ID))], Name: c.Names, Image: c.Image, State: c.State, Status: c.Status, Ports: c.Ports,
			Stack: labels["com.docker.compose.project"], Service: labels["com.docker.compose.service"],
			Update: imageUpdates.available(c.Image),
		})
	}
	return containers, nil
}

// readFile reads a stack's file, refusing anything but a plain file (not a link elsewhere) of sane size.
func readFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return "", fmt.Errorf("%s isn't a file Bastion will read", path)
	}
	raw, err := os.ReadFile(path)
	return string(raw), err
}

func stackFiles(ctx context.Context, stacksDir string, args protocol.StackArgs) (protocol.StackFiles, error) {
	s, err := findStack(ctx, stacksDir, args.Name)
	if err != nil {
		return protocol.StackFiles{}, err
	}
	compose, err := readFile(s.configFile)
	if err != nil {
		return protocol.StackFiles{}, err
	}
	files := protocol.StackFiles{Compose: compose}
	if env, err := readFile(filepath.Join(s.dir, ".env")); err == nil {
		files.Env = &env
	}
	return files, nil
}

// validate checks a compose file (and .env) as Docker would read them, from copies beside the real ones
// (so relative paths and the stack's other files work), without changing anything.
func validate(ctx context.Context, dir, name, compose string, env *string) error {
	if len(compose) > maxFileSize || (env != nil && len(*env) > maxFileSize) {
		return errors.New("that file is too big")
	}
	tmp, err := os.MkdirTemp(dir, ".bastion-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "compose.yaml")
	if err := os.WriteFile(file, []byte(compose), 0o600); err != nil {
		return err
	}
	args := []string{"compose", "-p", name, "--project-directory", dir, "-f", file}
	if env != nil {
		envFile := filepath.Join(tmp, ".env")
		if err := os.WriteFile(envFile, []byte(*env), 0o600); err != nil {
			return err
		}
		args = append(args, "--env-file", envFile)
	}
	_, err = docker(ctx, dir, append(args, "config", "--quiet")...)
	return err
}

// writeFile replaces a file in one step (a new file renamed over the old), keeping its permissions; a
// new .env is readable by root only.
func writeFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s isn't a file Bastion will write", path)
		}
		mode = info.Mode().Perm()
	} else if filepath.Base(path) == ".env" {
		mode = 0o600
	}
	tmp := path + ".bastion-new"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveStack(ctx context.Context, stacksDir string, args protocol.StackSaveArgs) error {
	s, err := findStack(ctx, stacksDir, args.Name)
	if err != nil {
		return err
	}
	if err := validate(ctx, s.dir, s.name, args.Compose, args.Env); err != nil {
		return err
	}
	if err := writeFile(s.configFile, args.Compose); err != nil {
		return err
	}
	if args.Env != nil {
		return writeFile(filepath.Join(s.dir, ".env"), *args.Env)
	}
	return nil
}

func createStack(ctx context.Context, stacksDir string, args protocol.StackSaveArgs) (protocol.ActionResult, error) {
	if !stackName.MatchString(args.Name) {
		return protocol.ActionResult{}, errors.New("a stack name is lower-case letters, numbers, - and _")
	}
	if _, err := findStack(ctx, stacksDir, args.Name); err == nil {
		return protocol.ActionResult{}, fmt.Errorf("there's already a stack called %s", args.Name)
	}
	dir := filepath.Join(stacksDir, args.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return protocol.ActionResult{}, err
	}
	if err := validate(ctx, dir, args.Name, args.Compose, args.Env); err != nil {
		os.Remove(dir) // only if it's still empty
		return protocol.ActionResult{}, err
	}
	if err := writeFile(filepath.Join(dir, "compose.yaml"), args.Compose); err != nil {
		return protocol.ActionResult{}, err
	}
	if args.Env != nil {
		if err := writeFile(filepath.Join(dir, ".env"), *args.Env); err != nil {
			return protocol.ActionResult{}, err
		}
	}
	return stackAction(ctx, stacksDir, protocol.StackActionArgs{Name: args.Name, Action: "up"})
}

var stackActions = map[string][]string{
	"up":      {"up", "--detach", "--remove-orphans"},
	"down":    {"down"},
	"stop":    {"stop"},
	"start":   {"start"},
	"restart": {"restart"},
	"pull":    {"pull"},
	"update":  {"up", "--detach", "--pull", "always", "--remove-orphans"}, // pull, then recreate what changed
}

func stackAction(ctx context.Context, stacksDir string, args protocol.StackActionArgs) (protocol.ActionResult, error) {
	verb, ok := stackActions[args.Action]
	if !ok {
		return protocol.ActionResult{}, fmt.Errorf("unknown action %q", args.Action)
	}
	s, err := findStack(ctx, stacksDir, args.Name)
	if err != nil {
		return protocol.ActionResult{}, err
	}
	out, err := docker(ctx, s.dir, append([]string{"compose", "-p", s.name, "-f", s.configFile}, verb...)...)
	return protocol.ActionResult{Output: out}, err
}

func containerAction(ctx context.Context, args protocol.ContainerActionArgs) (protocol.ActionResult, error) {
	if !containerID.MatchString(args.ID) {
		return protocol.ActionResult{}, errors.New("that isn't a container")
	}
	switch args.Action {
	case "start", "stop", "restart":
		out, err := docker(ctx, "", args.Action, args.ID)
		return protocol.ActionResult{Output: out}, err
	}
	return protocol.ActionResult{}, fmt.Errorf("unknown action %q", args.Action)
}

// logsCommand is the docker command for a stack's or a container's logs.
func logsCommand(ctx context.Context, stacksDir string, args protocol.LogsArgs, follow bool) ([]string, string, error) {
	tail := fmt.Sprint(min(max(args.Tail, 1), 5000))
	switch {
	case args.Stack != "":
		s, err := findStack(ctx, stacksDir, args.Stack)
		if err != nil {
			return nil, "", err
		}
		cmd := []string{"compose", "-p", s.name, "-f", s.configFile, "logs", "--no-color", "--tail", tail}
		if follow {
			cmd = append(cmd, "--follow")
		}
		return cmd, s.dir, nil
	case containerID.MatchString(args.Container):
		cmd := []string{"logs", "--tail", tail}
		if follow {
			cmd = append(cmd, "--follow")
		}
		return append(cmd, args.Container), "", nil
	}
	return nil, "", errors.New("say which stack or container")
}

func stackLogs(ctx context.Context, stacksDir string, args protocol.LogsArgs) (protocol.LogLines, error) {
	cmd, dir, err := logsCommand(ctx, stacksDir, args, false)
	if err != nil {
		return protocol.LogLines{}, err
	}
	out, err := docker(ctx, dir, cmd...)
	if err != nil {
		return protocol.LogLines{}, err
	}
	return protocol.LogLines{Lines: strings.Split(out, "\n")}, nil
}

// watchDocker signals changed whenever Docker reports a container starting, stopping or changing, until
// ctx ends. If the event stream drops (Docker restarting, say) it starts it again.
func watchDocker(ctx context.Context, changed chan<- struct{}) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "docker", "events", "--filter", "type=container", "--format", "{{.Status}}")
		out, err := cmd.StdoutPipe()
		if err == nil && cmd.Start() == nil {
			scan := bufio.NewScanner(out)
			for scan.Scan() {
				select {
				case changed <- struct{}{}:
				default: // a change is already waiting to be sent
				}
			}
			_ = cmd.Wait()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}
