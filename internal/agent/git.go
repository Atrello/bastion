package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Atrello/bastion/internal/protocol"
)

// Git-backed stacks: the agent keeps a checkout of the stack's repository in /var/lib/bastion/git/<stack>
// and copies the compose file from it into the stack's folder. Only https and ssh repositories are
// allowed, with the panel's deploy key for that stack. The .env file is never touched.

var (
	gitBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	gitURL    = regexp.MustCompile(`^(https://[^\s@]+|ssh://[^\s]+|[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^\s]+)$`)
)

func gitDir() string {
	if d := os.Getenv("BASTION_AGENT_GIT_DIR"); d != "" {
		return d
	}
	return "/var/lib/bastion/git"
}

// checkGit refuses repository addresses, branches and paths that could reach anything but the file asked for.
func checkGit(a protocol.GitArgs) error {
	if !gitURL.MatchString(a.Repo) || strings.HasPrefix(a.Repo, "-") {
		return errors.New("the repository's address should be https://… or git@host:owner/repo.git")
	}
	if !gitBranch.MatchString(a.Branch) || strings.Contains(a.Branch, "..") {
		return errors.New("that isn't a branch name")
	}
	clean := filepath.Clean(a.Path)
	if a.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("the compose file's path is inside the repository, like stacks/immich/compose.yaml")
	}
	return nil
}

// gitCommand runs git with the deploy key, allowing only https and ssh, with no prompts.
func gitCommand(ctx context.Context, dir, key string, args ...string) (string, error) {
	if err := os.MkdirAll(gitDir(), 0o700); err != nil {
		return "", err
	}
	keyDir, err := os.MkdirTemp(gitDir(), ".key-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(keyDir)
	keyFile := filepath.Join(keyDir, "key")
	if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -i "+keyFile+" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="+filepath.Join(gitDir(), "known_hosts"),
	)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git isn't installed on this server: install it (apt install git) to use Git-backed stacks")
		}
		return text, fmt.Errorf("git: %s", text)
	}
	return text, nil
}

// gitHead is the commit the branch points at in the repository now.
func gitHead(ctx context.Context, a protocol.GitArgs) (protocol.GitResult, error) {
	if err := checkGit(a); err != nil {
		return protocol.GitResult{}, err
	}
	out, err := gitCommand(ctx, "", a.Key, "ls-remote", "--", a.Repo, "refs/heads/"+a.Branch)
	if err != nil {
		return protocol.GitResult{}, err
	}
	commit, _, _ := strings.Cut(out, "\t")
	if len(commit) != 40 {
		return protocol.GitResult{}, fmt.Errorf("there's no branch called %s", a.Branch)
	}
	return protocol.GitResult{Commit: commit}, nil
}

// gitSync brings the stack's checkout up to the branch and copies its compose file into the stack (a new
// stack's folder in the stacks directory if it hasn't one).
func gitSync(ctx context.Context, stacksDir string, a protocol.GitArgs) (protocol.GitResult, error) {
	if err := checkGit(a); err != nil {
		return protocol.GitResult{}, err
	}
	if !stackName.MatchString(a.Stack) {
		return protocol.GitResult{}, errors.New("that isn't a stack name")
	}
	checkout := filepath.Join(gitDir(), a.Stack)
	if _, err := os.Stat(filepath.Join(checkout, ".git")); err != nil {
		if _, err := gitCommand(ctx, "", a.Key, "clone", "--quiet", "--depth", "1", "--branch", a.Branch, "--", a.Repo, checkout); err != nil {
			return protocol.GitResult{}, err
		}
	} else {
		steps := [][]string{
			{"remote", "set-url", "origin", a.Repo},
			{"fetch", "--quiet", "--depth", "1", "origin", "refs/heads/" + a.Branch},
			{"reset", "--quiet", "--hard", "FETCH_HEAD"},
		}
		for _, step := range steps {
			if _, err := gitCommand(ctx, checkout, a.Key, step...); err != nil {
				return protocol.GitResult{}, err
			}
		}
	}
	commit, err := gitCommand(ctx, checkout, a.Key, "rev-parse", "HEAD")
	if err != nil {
		return protocol.GitResult{}, err
	}
	// The compose file must be a plain file inside the checkout, not a link out of it.
	compose, err := readFile(filepath.Join(checkout, filepath.Clean(a.Path)))
	if err != nil {
		return protocol.GitResult{}, fmt.Errorf("%s isn't a file in the repository", a.Path)
	}

	var target string
	if s, err := findStack(ctx, stacksDir, a.Stack); err == nil {
		target = s.configFile
	} else {
		dir := filepath.Join(stacksDir, a.Stack)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return protocol.GitResult{}, err
		}
		target = filepath.Join(dir, "compose.yaml")
	}
	if err := validate(ctx, filepath.Dir(target), a.Stack, compose, nil); err != nil {
		return protocol.GitResult{}, fmt.Errorf("the compose file at %s doesn't check out: %w", commit[:7], err)
	}
	return protocol.GitResult{Commit: commit}, writeFile(target, compose)
}
