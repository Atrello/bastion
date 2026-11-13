package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Atrello/bastion/internal/protocol"
)

// A stack's backups: made by its agent (stopped first, as its settings say), the newest few kept with
// locked ones spared, restored as a job, made on schedule. Only the owner can.
func TestStackBackups(t *testing.T) {
	b := newPanel(t)
	b.setUp()
	asked := fakeAgent(t, b)
	base := "/api/servers/1/stacks/immich/backups"
	ops := func(op string) (n int) {
		for _, m := range asked() {
			var a protocol.StackBackupArgs
			if m.Op == protocol.OpStackBackup && json.Unmarshal(m.Args, &a) == nil && a.Op == op {
				n++
			}
		}
		return n
	}
	waitDone := func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var making int
			_ = b.api.db.QueryRow(`SELECT COUNT(*) FROM stack_backups WHERE state = 'making'`).Scan(&making)
			if making == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("a backup was still being made")
	}

	if status, r := b.call(b.client, "PUT", base+"/settings", map[string]any{"when": "0 4 * * *", "keep": 2, "stop": true, "storeId": 0}); status != 204 {
		t.Fatalf("settings: %d %v", status, r)
	}
	if status, _ := b.call(b.client, "PUT", base+"/settings", map[string]any{"when": "every day", "keep": 2, "stop": true, "storeId": 0}); status != 400 {
		t.Fatal("a schedule that isn't one was saved")
	}
	status, made := b.call(b.client, "POST", base, map[string]string{"name": "Before the upgrade"})
	if status != 200 || !strings.HasPrefix(made["name"].(string), "Before the upgrade-") {
		t.Fatalf("making one: %d %v", status, made)
	}
	waitDone()
	var first protocol.StackBackupArgs
	for _, m := range asked() {
		if m.Op == protocol.OpStackBackup {
			_ = json.Unmarshal(m.Args, &first)
		}
	}
	if first.Stack != "immich" || first.Op != "make" || !first.Stop || !strings.HasPrefix(first.Name, "before-the-upgrade-") {
		t.Fatalf("asked the agent: %+v", first)
	}
	// Lock the first, make three more: two newest unlocked are kept, plus the locked one.
	b.call(b.client, "PUT", fmt.Sprintf("%s/%v", base, made["id"]), map[string]bool{"locked": true})
	for i := 0; i < 3; i++ {
		b.call(b.client, "POST", base, nil)
		waitDone()
	}
	time.Sleep(100 * time.Millisecond)
	_, list := b.call(b.client, "GET", base, nil)
	backups := list["backups"].([]any)
	if len(backups) != 3 || ops("delete") != 1 {
		t.Fatalf("kept %d (deleted %d): %v", len(backups), ops("delete"), backups)
	}
	if last := backups[len(backups)-1].(map[string]any); last["locked"] != true {
		t.Fatalf("the locked one went: %v", backups)
	}
	if status, _ := b.call(b.client, "DELETE", fmt.Sprintf("%s/%v", base, made["id"]), nil); status != 400 {
		t.Fatal("a locked backup was deleted")
	}
	settings := list["settings"].(map[string]any)
	if settings["when"] != "0 4 * * *" || settings["nextRun"] == "" {
		t.Fatalf("settings: %v", settings)
	}

	// Restoring is a job.
	newest := backups[0].(map[string]any)
	if status, job := b.call(b.client, "POST", fmt.Sprintf("%s/%v/restore", base, newest["id"]), nil); status != 202 || job["title"] != "Restoring immich" {
		t.Fatalf("restoring: %d %v", status, job)
	}

	// On schedule.
	before := ops("make")
	b.api.runStackBackupSchedules(context.Background(), time.Date(2026, 11, 13, 4, 0, 0, 0, time.Local))
	waitDone()
	if ops("make") != before+1 {
		t.Fatal("the schedule didn't make a backup")
	}
	b.api.runStackBackupSchedules(context.Background(), time.Date(2026, 11, 13, 5, 0, 0, 0, time.Local))
	if ops("make") != before+1 {
		t.Fatal("a backup was made when none was due")
	}
	if status, _ := b.call(b.newClient(), "GET", base, nil); status != 401 {
		t.Fatal("signed out could see them")
	}
}
