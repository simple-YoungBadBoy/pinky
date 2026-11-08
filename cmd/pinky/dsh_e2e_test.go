//go:build e2e && !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DIYgod/pinky"
	"github.com/DIYgod/pinky/internal/agents"
)

// TestDeepSeekHarnessInTheApp drives the e2e build of the desktop app over
// MyGo's automation socket with a home folder that holds a DeepSeek Harness
// home (~/.dsh) and nobody's commands: Connect in Settings › Agents adds
// Pinky to the harness's patch layer, keeping the person's own patch and
// comment, a copy of the file, and Pinky's skill; Remove takes it out again
// and leaves the file as it was. It opens windows, so it needs a screen.
func TestDeepSeekHarnessInTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("opens the app")
	}
	bin := filepath.Join(t.TempDir(), "pinky")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	self, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	home := pinkyHome(t)
	userHome := filepath.Join(home, "user")
	dsh := filepath.Join(userHome, ".dsh")
	patch := filepath.Join(dsh, "cordis.patch.yml")
	const before = "# my own patches\n- id: ui-settings-general\n  config:\n    welcomeNoticeVersion: 1\n"
	if err := os.MkdirAll(dsh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend(t)
	// DSH_HOME would otherwise point at the harness this test runs in.
	app := startApp(t, bin, home, backend.URL, filepath.Join(home, "opened.txt"),
		"HOME="+userHome, "DSH_HOME="+dsh, agents.PathEnv+"="+filepath.Join(home, "bin"), "MYGO_LOCALE=en")
	win := app.window(t)

	// The sidebar names what it found, and nothing has changed yet.
	app.waitText(t, win, "Pinky found DeepSeek Harness on this computer.")
	if b, _ := os.ReadFile(patch); string(b) != before {
		t.Fatalf("cordis.patch.yml changed before a click:\n%s", b)
	}

	// Connect: Pinky's item goes in, the person's patch and comment stay.
	// The sidebar's card goes away once an agent can reach Pinky, so wait
	// for the file and read what the page says afterwards.
	app.press(t, win, "Connect DeepSeek Harness")
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(string(mustRead(t, patch)), "id: mcp-pinky") {
		if time.Now().After(deadline) {
			t.Fatalf("cordis.patch.yml after Connect:\n%s", mustRead(t, patch))
		}
		time.Sleep(100 * time.Millisecond)
	}
	app.press(t, win, "Settings")
	app.waitText(t, win, "Agents")
	app.pressAny(t, win, "Agents")
	app.waitText(t, win, "Connected. Quit DeepSeek Harness and open it again")
	got, _ := os.ReadFile(patch)
	if !strings.HasPrefix(string(got), before) ||
		!strings.Contains(string(got), "id: mcp-pinky") ||
		!strings.Contains(string(got), "command: "+self) {
		t.Fatalf("cordis.patch.yml:\n%s", got)
	}
	copies, _ := filepath.Glob(filepath.Join(home, "data", "agents", "backups", "deepseek-harness", "*-cordis.patch.yml"))
	if len(copies) != 1 {
		t.Fatalf("copies of cordis.patch.yml: %v", copies)
	}
	if b, _ := os.ReadFile(copies[0]); string(b) != before {
		t.Errorf("the copy:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dsh, "skills", "pinky", "SKILL.md")); string(b) != pinky.Skill {
		t.Error("DeepSeek Harness lacks Pinky's skill")
	}

	// Remove takes Pinky out and leaves the file as it was.
	app.press(t, win, "Remove Pinky from DeepSeek Harness")
	app.waitText(t, win, "Removed.")
	if b, _ := os.ReadFile(patch); string(b) != before {
		t.Errorf("cordis.patch.yml after Remove:\n%s", b)
	}

	run(t, bin, home, "quit")
	app.wait(t)
}

// mustRead is a file's content, or nothing when it is not there yet.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, _ := os.ReadFile(path)
	return b
}

// pressAny presses the first button or tab labelled label: the settings
// sections are tabs, and the page under them has buttons with other names.
func (a *uiApp) pressAny(t *testing.T, win int, label string) {
	t.Helper()
	for _, n := range a.nodes(t, win) {
		if n.Label == label && (n.Role == "button" || n.Role == "tab") {
			a.call(t, "act", map[string]any{"window": win, "id": n.ID, "action": "press"}, nil)
			return
		}
	}
	t.Fatalf("no %q to press", label)
}
