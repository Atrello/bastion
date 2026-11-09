package agent

import (
	"archive/zip"
	"bytes"
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

func TestConfigFiles(t *testing.T) {
	t.Setenv("BASTION_AGENT_GAMES_DIR", t.TempDir())
	_ = os.MkdirAll(gameDir(2), 0o755)
	put := func(name, data string) { _ = os.WriteFile(filepath.Join(gameDir(2), name), []byte(data), 0o644) }
	put("server.properties", "#Minecraft\nserver-port=25565\nmotd=Hi\n")
	put("config.yml", "listeners:\n- host: 0.0.0.0:25577\nsettings:\n  port: 1\n  name: x\nport: 9\n")
	put("config.json", `{"server":{"port":1,"names":["a","b"]}}`)
	err := applyConfigFiles(2, []protocol.ConfigFile{
		{File: "server.properties", Parser: "properties", Find: map[string]string{"server-port": "25570", "query.port": "25570"}},
		{File: "config.yml", Parser: "yaml", Find: map[string]string{"settings.port": "25571"}},
		{File: "config.json", Parser: "json", Find: map[string]string{"server.port": "25572", "server.names[1]": "c"}},
		{File: "../escape", Parser: "file", Find: map[string]string{"x": "y"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string { raw, _ := os.ReadFile(filepath.Join(gameDir(2), name)); return string(raw) }
	if got := read("server.properties"); got != "#Minecraft\nserver-port=25570\nmotd=Hi\nquery.port=25570\n" {
		t.Errorf("properties: %q", got)
	}
	if got := read("config.yml"); got != "listeners:\n- host: 0.0.0.0:25577\nsettings:\n  port: 25571\n  name: x\nport: 9\n" {
		t.Errorf("yaml: %q", got)
	}
	if got := read("config.json"); !strings.Contains(got, `"port": 25572`) || !strings.Contains(got, `"c"`) {
		t.Errorf("json: %q", got)
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	base := t.TempDir()
	t.Setenv("BASTION_AGENT_GAMES_DIR", filepath.Join(base, "games"))
	_ = os.MkdirAll(filepath.Join(gameDir(3), "world", "region"), 0o755)
	_ = os.WriteFile(filepath.Join(gameDir(3), "world", "level.dat"), []byte("level"), 0o644)
	_ = os.WriteFile(filepath.Join(gameDir(3), "world", "region", "r.0.0.mca"), []byte("chunks"), 0o644)
	_ = os.WriteFile(filepath.Join(gameDir(3), "server.properties"), []byte("motd=Hi"), 0o644)

	do := func(a protocol.GameFileArgs) error { a.ID = 3; _, err := gameFiles(a); return err }
	if err := do(protocol.GameFileArgs{Op: "archive", Path: "/", Paths: []string{"world", "server.properties"}, To: "/backup.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	if err := do(protocol.GameFileArgs{Op: "archive", Path: "/", Paths: []string{"world"}, To: "/backup.tar.gz"}); err == nil {
		t.Fatal("an archive was made over an existing file")
	}
	_ = os.MkdirAll(filepath.Join(gameDir(3), "restored"), 0o755)
	_ = os.Rename(filepath.Join(gameDir(3), "backup.tar.gz"), filepath.Join(gameDir(3), "restored", "backup.tar.gz"))
	if err := do(protocol.GameFileArgs{Op: "unarchive", Path: "/restored/backup.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(gameDir(3), "restored", "world", "region", "r.0.0.mca")); string(raw) != "chunks" {
		t.Fatalf("unpacked: %q", raw)
	}

	// A zip that tries to write outside the game's folder, and holds a link, unpacks safely.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"../../escaped.txt", "/etc/also-escaped.txt", "ok/inside.txt"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("x"))
	}
	link := &zip.FileHeader{Name: "link"}
	link.SetMode(os.ModeSymlink | 0o777)
	w, _ := zw.CreateHeader(link)
	_, _ = w.Write([]byte("/etc/passwd"))
	_ = zw.Close()
	_ = os.WriteFile(filepath.Join(gameDir(3), "mods.zip"), buf.Bytes(), 0o644)
	if err := do(protocol.GameFileArgs{Op: "unarchive", Path: "/mods.zip"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.txt")); err == nil {
		t.Fatal("a zip wrote outside the game's folder")
	}
	if _, err := os.Lstat(filepath.Join(gameDir(3), "link")); err == nil {
		t.Fatal("a link was unpacked")
	}
	for _, p := range []string{"escaped.txt", "etc/also-escaped.txt", "ok/inside.txt"} {
		if _, err := os.Stat(filepath.Join(gameDir(3), p)); err != nil {
			t.Errorf("%s should be inside the game's folder: %v", p, err)
		}
	}
}
