package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atrello/bastion/internal/protocol"
)

// A real stack's folder and volume backed up, changed, restored (owners, permissions and links too), and
// restored again after its volume was deleted. It needs Docker and the alpine image, so it runs only with
// BASTION_TEST_DOCKER=1 (docs/development.md).
func TestStackBackupWithDocker(t *testing.T) {
	if os.Getenv("BASTION_TEST_DOCKER") != "1" {
		t.Skip("set BASTION_TEST_DOCKER=1 to run it against Docker")
	}
	ctx := context.Background()
	base := t.TempDir()
	t.Setenv("BASTION_AGENT_BACKUPS_DIR", filepath.Join(base, "backups"))
	stacksDir := filepath.Join(base, "stacks")
	dir := filepath.Join(stacksDir, "zzbk")
	_ = os.MkdirAll(filepath.Join(dir, "local"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  app:\n    image: mirror.gcr.io/library/alpine:3.20\n    command: sleep 3600\n    volumes:\n      - data:/data\n      - ./local:/local\nvolumes:\n  data:\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "local", "notes.txt"), []byte("before"), 0o644)
	run := func(args ...string) string {
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "up", "-d")
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "down", "-v").Run()
	})
	run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "exec", "-T", "app", "sh", "-c", "echo secret-db > /data/db && chown 999:999 /data/db && chmod 600 /data/db && ln -s db /data/latest")

	tr := newTracker(ctx, nil)
	res, err := stackBackup(ctx, stacksDir, protocol.StackBackupArgs{Stack: "zzbk", Op: "make", Name: "b-1.tar.gz", Stop: true}, tr)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("backup %d bytes, %d of %d read", res.Size, tr.done.Load(), tr.total.Load())
	if running := run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "ps", "-q", "--status", "running"); running == "" {
		t.Fatal("the stack wasn't started again after the backup")
	}

	// Change everything, then restore.
	run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "exec", "-T", "app", "sh", "-c", "echo griefed > /data/db && rm /data/latest && echo junk > /data/new")
	_ = os.WriteFile(filepath.Join(dir, "local", "notes.txt"), []byte("after"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "local", "extra.txt"), []byte("x"), 0o644)
	if _, err := stackBackup(ctx, stacksDir, protocol.StackBackupArgs{Stack: "zzbk", Op: "restore", Name: "b-1.tar.gz", Stop: true}, newTracker(ctx, nil)); err != nil {
		t.Fatal(err)
	}
	got := run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "exec", "-T", "app", "sh", "-c", "cat /data/db; stat -c '%u:%g %a' /data/db; readlink /data/latest; ls /data")
	t.Log(got)
	if got != "secret-db\n999:999 600\ndb\ndb\nlatest" {
		t.Fatalf("restored volume: %q", got)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "local", "notes.txt")); string(raw) != "before" {
		t.Fatalf("restored folder: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "local", "extra.txt")); err == nil {
		t.Fatal("a file made since the backup survived the restore")
	}

	// Restore a stack whose volume has gone: made again.
	run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "down", "-v")
	if _, err := stackBackup(ctx, stacksDir, protocol.StackBackupArgs{Stack: "zzbk", Op: "restore", Name: "b-1.tar.gz"}, nil); err != nil {
		t.Fatal(err)
	}
	run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "up", "-d")
	if got := run("compose", "-p", "zzbk", "-f", filepath.Join(dir, "compose.yaml"), "exec", "-T", "app", "cat", "/data/db"); got != "secret-db" {
		t.Fatalf("a volume made again: %q", got)
	}
	if _, err := stackBackup(ctx, stacksDir, protocol.StackBackupArgs{Stack: "zzbk", Op: "delete", Name: "b-1.tar.gz"}, nil); err != nil {
		t.Fatal(err)
	}
}
