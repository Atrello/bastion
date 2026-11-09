package agent

import (
	"github.com/Atrello/bastion/internal/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelAddress(t *testing.T) {
	for addr, ok := range map[string]bool{
		"https://bastion.home.lab":   true,
		"https://bastion.home.lab/":  true,
		"http://192.168.1.10:8080":   true, // a private network
		"http://localhost:8080":      true,
		"http://bastion.example.com": false, // plain http on a public name
		"http://8.8.8.8":             false,
		"ftp://bastion.home.lab":     false,
		"https://bastion.home.lab/x": false,
		"bastion.home.lab":           false,
	} {
		if _, err := CheckPanelAddress(addr, false); (err == nil) != ok {
			t.Errorf("%s: allowed=%v, want %v (%v)", addr, err == nil, ok, err)
		}
	}
	if _, err := CheckPanelAddress("http://bastion.example.com", true); err != nil {
		t.Error("--insecure should allow plain http")
	}
}

func TestStackNames(t *testing.T) {
	for _, name := range []string{"immich", "home-assistant", "a_b", "x1"} {
		if !stackName.MatchString(name) {
			t.Errorf("%s should be a stack name", name)
		}
	}
	for _, name := range []string{"", "..", "../etc", "a/b", "Immich", "-x", "a b", strings.Repeat("a", 64)} {
		if stackName.MatchString(name) {
			t.Errorf("%q shouldn't be a stack name", name)
		}
	}
}

func TestFilesAreOnlyPlainFiles(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	_ = os.WriteFile(secret, []byte("root's secret"), 0o600)
	link := filepath.Join(dir, "compose.yaml")
	_ = os.Symlink(secret, link)

	if _, err := readFile(link); err == nil {
		t.Fatal("read through a link to another file")
	}
	if err := writeFile(link, "services: {}"); err == nil {
		t.Fatal("wrote through a link to another file")
	}
	if raw, _ := os.ReadFile(secret); string(raw) != "root's secret" {
		t.Fatal("the linked file was changed")
	}

	env := filepath.Join(dir, ".env")
	if err := writeFile(env, "A=1\n"); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(env); info.Mode().Perm() != 0o600 {
		t.Fatalf("a new .env is %v, want readable by its owner only", info.Mode().Perm())
	}
	big := filepath.Join(dir, "big.yaml")
	_ = os.WriteFile(big, make([]byte, maxFileSize+1), 0o644)
	if _, err := readFile(big); err == nil {
		t.Fatal("read a file over the size limit")
	}
}

func TestParseRef(t *testing.T) {
	for image, want := range map[string]string{
		"alpine":                              "registry-1.docker.io library/alpine latest",
		"alpine:3.20":                         "registry-1.docker.io library/alpine 3.20",
		"louislam/uptime-kuma:1":              "registry-1.docker.io louislam/uptime-kuma 1",
		"docker.io/library/redis:7":           "registry-1.docker.io library/redis 7",
		"ghcr.io/immich-app/immich-server:v1": "ghcr.io immich-app/immich-server v1",
		"localhost:5000/mine":                 "localhost:5000 mine latest",
	} {
		r, repo, tag, err := parseRef(image)
		if got := r + " " + repo + " " + tag; err != nil || got != want {
			t.Errorf("%s: got %q (%v), want %q", image, got, err, want)
		}
	}
	if _, _, _, err := parseRef("Bad Image"); err == nil {
		t.Error("a bad reference was accepted")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]uint64{"0B": 0, "512B": 512, "12.2MB": 12_200_000, "1.5GB": 1_500_000_000, "3kB": 3000, "?": 0} {
		if got := parseSize(in); got != want {
			t.Errorf("%s: got %d, want %d", in, got, want)
		}
	}
}

func TestGitChecks(t *testing.T) {
	ok := protocol.GitArgs{Repo: "https://github.com/acme/homelab.git", Branch: "main", Path: "stacks/immich/compose.yaml"}
	if err := checkGit(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []protocol.GitArgs{
		{Repo: "file:///etc", Branch: "main", Path: "x"},
		{Repo: "ext::sh -c touch% /tmp/pwned", Branch: "main", Path: "x"},
		{Repo: "--upload-pack=touch /tmp/x", Branch: "main", Path: "x"},
		{Repo: ok.Repo, Branch: "-x", Path: "x"},
		{Repo: ok.Repo, Branch: "main", Path: "../../etc/shadow"},
		{Repo: ok.Repo, Branch: "main", Path: "/etc/shadow"},
	} {
		if checkGit(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if checkGit(protocol.GitArgs{Repo: "git@github.com:acme/homelab.git", Branch: "release/1.0", Path: "compose.yaml"}) != nil {
		t.Error("refused an ssh address")
	}
}
