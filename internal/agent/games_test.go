package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atrello/bastion/internal/protocol"
)

// A game's file manager must never reach outside the game's folder, however the path is written.
func TestGameFilesStayInside(t *testing.T) {
	base := t.TempDir()
	t.Setenv("BASTION_AGENT_GAMES_DIR", filepath.Join(base, "games"))
	t.Setenv("BASTION_AGENT_BACKUPS_DIR", filepath.Join(base, "backups"))
	if err := os.MkdirAll(gameDir(1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret"), []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link a game could have made, pointing out of its folder.
	if err := os.Symlink(filepath.Join(base, "secret"), filepath.Join(gameDir(1), "link")); err != nil {
		t.Fatal(err)
	}

	write := func(p, data string) error {
		_, err := gameFiles(protocol.GameFileArgs{ID: 1, Op: "write", Path: p, Data: []byte(data)})
		return err
	}
	if err := write("server.properties", "motd=Hello"); err != nil {
		t.Fatal(err)
	}
	// "../secret" is cleaned to "secret" inside the folder, never the file beside it.
	_ = write("../secret", "changed")
	_ = write("/../../secret", "changed")
	if raw, _ := os.ReadFile(filepath.Join(base, "secret")); string(raw) != "not yours" {
		t.Fatalf("a path left the game's folder: %q", raw)
	}
	for _, op := range []protocol.GameFileArgs{{ID: 1, Op: "read", Path: "link"}, {ID: 1, Op: "download", Path: "link"}, {ID: 1, Op: "write", Path: "link", Data: []byte("x")}} {
		if res, err := gameFiles(op); err == nil && strings.Contains(string(res.Data), "not yours") {
			t.Fatalf("%s followed a link out", op.Op)
		}
	}
	if raw, _ := os.ReadFile(filepath.Join(base, "secret")); string(raw) != "not yours" {
		t.Fatalf("a link let a write out: %q", raw)
	}
	if _, err := gameFiles(protocol.GameFileArgs{ID: 1, Op: "delete", Path: "/"}); err == nil {
		t.Fatal("the game's folder itself was deleted")
	}

	// A backup brings the files back after they're changed.
	if _, err := gameBackup(context.Background(), protocol.GameBackupArgs{ID: 1, Op: "make", Name: "first.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	_ = write("server.properties", "motd=Changed")
	_ = write("extra.txt", "new")
	if _, err := gameBackup(context.Background(), protocol.GameBackupArgs{ID: 1, Op: "make", Name: "../escape.tar.gz"}); err == nil {
		t.Fatal("a backup name with a path was accepted")
	}
	if _, err := gameBackup(context.Background(), protocol.GameBackupArgs{ID: 1, Op: "restore", Name: "first.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	res, err := gameFiles(protocol.GameFileArgs{ID: 1, Op: "read", Path: "server.properties"})
	if err != nil || string(res.Data) != "motd=Hello" {
		t.Fatalf("restored: %v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(gameDir(1), "extra.txt")); err == nil {
		t.Fatal("a file made after the backup survived restoring it")
	}
}

func TestCheckSpec(t *testing.T) {
	good := protocol.GameSpec{ID: 1, Image: "ghcr.io/pterodactyl/yolks:java_21", MemoryMB: 2048, Ports: []int{25565},
		Env: map[string]string{"SERVER_JARFILE": "server.jar"}, InstallImage: "ghcr.io/pterodactyl/installers:alpine", InstallEntrypoint: "ash", InstallScript: "echo hi"}
	if err := checkSpec(good); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*protocol.GameSpec){
		"an option for an image": func(s *protocol.GameSpec) { s.Image = "--privileged" },
		"a bad variable":         func(s *protocol.GameSpec) { s.Env = map[string]string{"A=B": "x"} },
		"no port":                func(s *protocol.GameSpec) { s.Ports = nil },
		"a port out of range":    func(s *protocol.GameSpec) { s.Ports = []int{70000} },
		"an odd entrypoint":      func(s *protocol.GameSpec) { s.InstallEntrypoint = "sh -c 'rm'" },
	} {
		s := good
		change(&s)
		if checkSpec(s) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
