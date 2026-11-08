package agents

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// dshMine is a patch layer of someone who patches their harness themselves:
// a comment, a patch of their own, and an entry of another MCP server.
const dshMine = `# The patches I keep.
- id: my-plugin
  name: '@acme/dsh-plugin'
  config:
    answer: 42
- insert:
    - id: mcp-github
      name: '@modelcontextprotocol/server-github'
      config:
        serverName: github
        transport: stdio
        command: npx
        args: [server-github]
`

// patchList decodes a patch layer to compare two of them by what they hold.
func patchList(t *testing.T, src string) []any {
	t.Helper()
	var list []any
	if err := yaml.Unmarshal([]byte(src), &list); err != nil {
		t.Fatalf("not a list of patches:\n%s\n%v", src, err)
	}
	return list
}

func TestConnectDeepSeekHarness(t *testing.T) {
	ts := newTestSetup(t)
	ts.write(t, ".dsh/cordis.patch.yml", dshMine)
	want := filepath.Join(ts.home, ".dsh", "cordis.patch.yml")
	if st := ts.status(t, "deepseek-harness"); !st.Found || st.State != NotConnected || st.Settings != want {
		t.Fatalf("before: %+v", st)
	}
	res, err := ts.Connect(context.Background(), "deepseek-harness")
	if err != nil {
		t.Fatal(err)
	}
	got := ts.read(t, ".dsh/cordis.patch.yml")
	for _, s := range []string{
		"# The patches I keep.",
		"- id: my-plugin",
		"name: '@acme/dsh-plugin'",
		"- id: mcp-github",
		"id: mcp-pinky",
		"@deepseek-ai/dsh-mcp-client",
		"serverName: pinky",
		"transport: stdio",
		ts.Server.Command,
	} {
		if !strings.Contains(got, s) {
			t.Errorf("cordis.patch.yml lacks %q:\n%s", s, got)
		}
	}
	if b := ts.backups(t, "deepseek-harness"); len(b) != 1 || b[0] != dshMine || len(res.Backups) != 1 {
		t.Errorf("backups %q, result %+v", b, res)
	}
	if skill := ts.read(t, ".dsh/skills/pinky/SKILL.md"); skill != testSkill {
		t.Errorf("skill %q", skill)
	}
	if st := ts.status(t, "deepseek-harness"); st.State != Connected || st.Command != ts.Server.Command+" mcp" {
		t.Errorf("after: %+v", st)
	}
	// Connecting again changes nothing and keeps no copy.
	if _, err := ts.Connect(context.Background(), "deepseek-harness"); err != nil {
		t.Fatal(err)
	}
	if again := ts.read(t, ".dsh/cordis.patch.yml"); again != got {
		t.Errorf("connecting twice rewrote the file:\n%s\nwant\n%s", again, got)
	}
	if b := ts.backups(t, "deepseek-harness"); len(b) != 1 {
		t.Errorf("%d backups after connecting twice", len(b))
	}
	if _, err := ts.Remove(context.Background(), "deepseek-harness"); err != nil {
		t.Fatal(err)
	}
	restored := ts.read(t, ".dsh/cordis.patch.yml")
	if got, want := patchList(t, restored), patchList(t, dshMine); !reflect.DeepEqual(got, want) {
		t.Errorf("after Remove:\n%s\nwant the patches\n%s", restored, dshMine)
	}
	if !strings.Contains(restored, "# The patches I keep.") {
		t.Errorf("Remove lost the comment:\n%s", restored)
	}
	if exists(filepath.Join(ts.home, ".dsh", "skills", "pinky")) {
		t.Error("the skill stayed")
	}
	if st := ts.status(t, "deepseek-harness"); st.State != NotConnected {
		t.Errorf("after Remove: %+v", st)
	}
}

// TestDeepSeekHarnessMakesTheFile connects to a harness that has no patch
// layer yet.
func TestDeepSeekHarnessMakesTheFile(t *testing.T) {
	ts := newTestSetup(t)
	ts.fakeCommand(t, "dsh")
	if st := ts.status(t, "deepseek-harness"); !st.Found || st.State != NotConnected {
		t.Fatalf("before: %+v", st)
	}
	res, err := ts.Connect(context.Background(), "deepseek-harness")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Backups) != 0 {
		t.Errorf("backups for a file that was not there: %q", res.Backups)
	}
	got := ts.read(t, ".dsh/cordis.patch.yml")
	if list := patchList(t, got); len(list) != 1 {
		t.Errorf("patch layer:\n%s", got)
	}
	if st := ts.status(t, "deepseek-harness"); st.State != Connected {
		t.Errorf("after: %+v", st)
	}
	if _, err := ts.Remove(context.Background(), "deepseek-harness"); err != nil {
		t.Fatal(err)
	}
	if list := patchList(t, ts.read(t, ".dsh/cordis.patch.yml")); len(list) != 0 {
		t.Errorf("the patch layer still has patches: %v", list)
	}
	if st := ts.status(t, "deepseek-harness"); st.State != NotConnected {
		t.Errorf("after Remove: %+v", st)
	}
}

// TestDeepSeekHarnessRefusesABrokenFile leaves a patch layer Pinky cannot
// read as it is.
func TestDeepSeekHarnessRefusesABrokenFile(t *testing.T) {
	for _, broken := range []string{"insert: [\n", "commands:\n  - echo\n"} {
		ts := newTestSetup(t)
		ts.write(t, ".dsh/cordis.patch.yml", broken)
		if st := ts.status(t, "deepseek-harness"); st.Problem == "" {
			t.Errorf("%q: no problem: %+v", broken, st)
		}
		if _, err := ts.Connect(context.Background(), "deepseek-harness"); err == nil {
			t.Errorf("%q: connected through a broken file", broken)
		}
		if got := ts.read(t, ".dsh/cordis.patch.yml"); got != broken {
			t.Errorf("%q: the broken file changed:\n%s", broken, got)
		}
		if b := ts.backups(t, "deepseek-harness"); len(b) != 0 {
			t.Errorf("%q: %d backups", broken, len(b))
		}
	}
}

// TestDeepSeekHarnessOutdated corrects an entry that starts another Pinky.
func TestDeepSeekHarnessOutdated(t *testing.T) {
	ts := newTestSetup(t)
	ts.write(t, ".dsh/cordis.patch.yml", `- insert:
    - id: mcp-pinky
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: pinky
        transport: stdio
        command: /old/Pinky
        args: [mcp]
`)
	if st := ts.status(t, "deepseek-harness"); st.State != Outdated || st.Why != WhyMissing {
		t.Errorf("before: %+v", st)
	}
	if _, err := ts.Connect(context.Background(), "deepseek-harness"); err != nil {
		t.Fatal(err)
	}
	if st := ts.status(t, "deepseek-harness"); st.State != Connected {
		t.Errorf("after: %+v", st)
	}
	// The entry is corrected where it is, in the patch that holds it.
	if got := ts.read(t, ".dsh/cordis.patch.yml"); strings.Contains(got, "/old/Pinky") ||
		strings.Count(got, "id: mcp-pinky") != 1 {
		t.Errorf("patch layer:\n%s", got)
	}
}

// TestDeepSeekHarnessKeepsIndentation leaves the file's own lines as they
// were, indented as they were, next to Pinky's entry.
func TestDeepSeekHarnessKeepsIndentation(t *testing.T) {
	for _, mine := range []string{
		"- id: my-plugin\n  name: '@acme/dsh-plugin'\n  config:\n    answer: 42\n",
		"- insert:\n    - id: my-plugin\n      name: '@acme/dsh-plugin'\n      config:\n        answer: 42\n",
	} {
		ts := newTestSetup(t)
		ts.write(t, ".dsh/cordis.patch.yml", mine)
		if _, err := ts.Connect(context.Background(), "deepseek-harness"); err != nil {
			t.Fatal(err)
		}
		if got := ts.read(t, ".dsh/cordis.patch.yml"); !strings.HasPrefix(got, mine) {
			t.Errorf("the person's patch was rewritten:\n%s\nwant it to start with\n%s", got, mine)
		}
		if _, err := ts.Remove(context.Background(), "deepseek-harness"); err != nil {
			t.Fatal(err)
		}
		if got := ts.read(t, ".dsh/cordis.patch.yml"); got != mine {
			t.Errorf("after Remove:\n%s\nwant\n%s", got, mine)
		}
	}
}
