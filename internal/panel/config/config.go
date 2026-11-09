// Package config reads the panel's settings from the environment. Every setting has a default that suits
// running it in its container, so most installs set only BASTION_PUBLIC_URL.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	// Where the panel listens, e.g. ":8080".
	Addr string
	// Where the database lives.
	DataDir string
	// The address people and agents use to reach the panel, e.g. https://bastion.home.lab. Passkeys,
	// cookies and the agents' join command depend on it.
	PublicURL *url.URL
	// Sessions end after this long without activity.
	SessionIdle time.Duration
	// Believe X-Forwarded-For from a reverse proxy in front of the panel (for sign-in limits and logs).
	TrustProxy bool
	// Where the agent's programs are, for new servers to download (the container image puts them in /agents).
	AgentBinaries string
	// The panel's version (the VERSION file, set when building), shown to those signed in.
	Version string
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:          env("BASTION_ADDR", ":8080"),
		DataDir:       env("BASTION_DATA_DIR", "data"),
		SessionIdle:   12 * time.Hour,
		TrustProxy:    env("BASTION_TRUST_PROXY", "") == "true",
		AgentBinaries: env("BASTION_AGENT_BINARIES", "bin"),
	}
	if v := env("BASTION_SESSION_IDLE", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			return nil, fmt.Errorf("BASTION_SESSION_IDLE: %q isn't a duration of a minute or more, like 12h", v)
		}
		cfg.SessionIdle = d
	}

	raw := env("BASTION_PUBLIC_URL", "")
	if raw == "" {
		return nil, errors.New("BASTION_PUBLIC_URL isn't set: it's the address the panel is reached at, e.g. https://bastion.home.lab")
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" {
		return nil, fmt.Errorf("BASTION_PUBLIC_URL: %q should be just the address, like https://bastion.home.lab", raw)
	}
	cfg.PublicURL = u

	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Secure says whether the panel is reached over HTTPS, so cookies can be marked Secure.
func (c *Config) Secure() bool { return c.PublicURL.Scheme == "https" }

func env(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}
