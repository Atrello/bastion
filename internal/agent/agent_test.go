package agent

import (
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
