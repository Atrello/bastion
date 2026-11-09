// Package agent is the program on each server: it connects out to the panel, reports what Docker is
// running, and does what the panel asks, from a fixed list (internal/protocol, docs/protocol.md).
package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// Config is the agent's settings, saved by Join in /etc/bastion/agent.json (only root can read it: the
// secret lets anyone pose as this server's agent).
type Config struct {
	Panel     string `json:"panel"`
	AgentID   string `json:"agentId"`
	Secret    string `json:"secret"`
	StacksDir string `json:"stacksDir"`
}

func ConfigPath() string {
	if p := os.Getenv("BASTION_AGENT_CONFIG"); p != "" {
		return p
	}
	return "/etc/bastion/agent.json"
}

func LoadConfig() (*Config, error) {
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("no settings at %s: join the panel first (bastion-agent join <panel> <token>)", ConfigPath())
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%s can't be read: %w", ConfigPath(), err)
	}
	if cfg.StacksDir == "" {
		cfg.StacksDir = "/opt/stacks"
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := path + ".new"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CheckPanelAddress refuses a panel address the agent shouldn't trust: plain http:// is only allowed on a
// private network (or when the person joining says so), as anyone in between could pose as the panel.
func CheckPanelAddress(panel string, insecure bool) (string, error) {
	u, err := url.Parse(strings.TrimRight(panel, "/"))
	if err != nil || u.Host == "" || u.Path != "" {
		return "", errors.New("the panel's address should look like https://bastion.home.lab")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !insecure && !privateHost(u.Hostname()) {
			return "", errors.New("the panel's address is plain http:// on a public address: use https://, or --insecure if you really mean it")
		}
	default:
		return "", errors.New("the panel's address starts with https://")
	}
	return u.String(), nil
}

func privateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

// Join swaps a one-time join token for this server's own agent ID and secret, and saves them.
func Join(panel, token string, insecure bool) error {
	panel, err := CheckPanelAddress(panel, insecure)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	body, _ := json.Marshal(protocol.EnrolRequest{Token: token, Hostname: hostname})
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Post(panel+"/api/agent/enrol", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("the panel can't be reached: %w", err)
	}
	defer res.Body.Close()
	var reply struct {
		protocol.EnrolReply
		Error string `json:"error"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 64<<10)).Decode(&reply)
	if res.StatusCode != http.StatusOK || reply.AgentID == "" || reply.Secret == "" {
		if reply.Error == "" {
			reply.Error = res.Status
		}
		return fmt.Errorf("the panel refused: %s", reply.Error)
	}
	return saveConfig(&Config{Panel: panel, AgentID: reply.AgentID, Secret: reply.Secret, StacksDir: "/opt/stacks"})
}

func hello() protocol.Hello {
	hostname, _ := os.Hostname()
	return protocol.Hello{
		Protocol: protocol.Version, Version: Version, Hostname: hostname, OS: osName(),
		Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Docker: dockerVersion(),
	}
}

// Version is set when building: -ldflags "-X github.com/Atrello/bastion/internal/agent.Version=0.1.0".
var Version = "dev"

// osName is the distribution's name, e.g. "Debian GNU/Linux 12 (bookworm)", from /etc/os-release.
func osName() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return runtime.GOOS
}
