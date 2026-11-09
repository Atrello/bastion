package eggs

import (
	"os"
	"strings"
	"testing"
)

// Pterodactyl's own Paper egg (testdata/egg-paper.json, from pterodactyl/panel, MIT licensed).
func TestPaper(t *testing.T) {
	raw, err := os.ReadFile("testdata/egg-paper.json")
	if err != nil {
		t.Fatal(err)
	}
	e, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "Paper" || len(e.Images) < 2 || e.Images[0].Ref != "ghcr.io/pterodactyl/yolks:java_25" || e.Config.Stop != "stop" {
		t.Fatalf("read as %+v", e)
	}
	files, err := e.ConfigFiles()
	if err != nil || len(files) != 1 || files[0].Parser != "properties" {
		t.Fatalf("config files: %+v %v", files, err)
	}
	p := Placeholders{Port: 25570, MemoryMB: 4096, Env: map[string]string{"SERVER_JARFILE": "server.jar"}}
	if got := p.Fill(files[0].Find["server-port"]); got != "25570" {
		t.Errorf("server-port: %q", got)
	}
	if got := p.Fill(e.Startup); !strings.Contains(got, "-jar server.jar") || strings.Contains(got, "{{") {
		t.Errorf("startup: %q", got)
	}
}

func TestRefused(t *testing.T) {
	for name, egg := range map[string]string{
		"not JSON":       `nope`,
		"no name":        `{"startup":"x","docker_images":{"a":"alpine"}}`,
		"no image":       `{"name":"x","startup":"x"}`,
		"an option":      `{"name":"x","startup":"x","docker_images":{"a":"--privileged"}}`,
		"a bad variable": `{"name":"x","startup":"x","docker_images":{"a":"alpine"},"variables":[{"env_variable":"A B"}]}`,
		"an odd shell":   `{"name":"x","startup":"x","docker_images":{"a":"alpine"},"scripts":{"installation":{"script":"x","container":"alpine","entrypoint":"bash -c rm"}}}`,
		"a config path":  `{"name":"x","startup":"x","docker_images":{"a":"alpine"},"config":{"files":"{\"../../etc/passwd\":{\"parser\":\"file\",\"find\":{}}}"}}`,
	} {
		if _, err := Parse([]byte(egg)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRules(t *testing.T) {
	v := Variable{Name: "Version", Rules: "required|string|max:5"}
	if v.Check("") == nil || v.Check("toolong") == nil || v.Check("1.21") != nil {
		t.Error("required|string|max:5")
	}
	n := Variable{Name: "Players", Rules: "required|integer|between:1,100"}
	if n.Check("0") == nil || n.Check("abc") == nil || n.Check("2.5") == nil || n.Check("20") != nil {
		t.Error("integer|between")
	}
	r := Variable{Name: "Jar", Rules: "required|regex:/^([\\w\\d._-]+)(\\.jar)$/"}
	if r.Check("server.jar") != nil || r.Check("server.sh") == nil {
		t.Error("regex")
	}
	if (Variable{Rules: "nullable|in:a,b"}).Check("") != nil || (Variable{Rules: "in:a,b"}).Check("c") == nil {
		t.Error("nullable|in")
	}
}
