package agents

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file changes DeepSeek Harness's home patch layer,
// $DSH_HOME/cordis.patch.yml: a YAML list of loader patches, where Pinky's MCP
// server is one item that inserts the harness's MCP client plugin. Unlike
// JSON and TOML, the file is a list rather than a table whose entry has a
// name, and people keep comments in it, so the change goes through yaml.Node,
// which keeps every node Pinky does not touch, comments included, as it was.
// As with JSON and TOML, the file is read again after the change and refused
// unless every other item reads the same.

// dshPatchID is the id of Pinky's patch item, and of the plugin entry the item
// inserts. An id of Pinky's own keeps it apart from the person's patches.
const dshPatchID = "mcp-" + Name

// dshPlugin is DeepSeek Harness's MCP client, which starts an MCP server and
// puts its tools in front of the model.
const dshPlugin = "@deepseek-ai/dsh-mcp-client"

// dshHome is where DeepSeek Harness keeps its settings: $DSH_HOME, else
// ~/.dsh.
func dshHome(e *Env) string {
	if d := e.getenv("DSH_HOME"); d != "" {
		return d
	}
	return filepath.Join(e.Home, ".dsh")
}

// dshPatch is DeepSeek Harness's home patch layer, which applies to every
// profile: the profile's own cordis.patch.yml comes first, this one after it.
func dshPatch(e *Env) string {
	return filepath.Join(dshHome(e), "cordis.patch.yml")
}

// dshSkills is the folder of skills DeepSeek Harness reads.
func dshSkills(e *Env) string {
	return filepath.Join(dshHome(e), "skills")
}

// dshCLI is DeepSeek Harness's command, which has no command for MCP servers:
// Pinky changes its patch layer instead.
var dshCLI = &cli{name: "dsh"}

// dshStore keeps Pinky's MCP server in DeepSeek Harness's home patch layer.
type dshStore struct{}

func (dshStore) read(file string) (any, bool, error) {
	v, ok, err := cached(file, func(data []byte) (any, error) { return readDSHPatch(data) })
	if err != nil || !ok {
		return nil, false, err
	}
	return v, v != nil, nil
}

func (dshStore) set(data []byte, srv Server) ([]byte, error) {
	return setDSHPatch(data, srv)
}

func (dshStore) unset(data []byte) ([]byte, bool, error) {
	return deleteDSHPatch(data)
}

// dshEntry is Pinky's item: one insert of the MCP client plugin, which starts
// this Pinky.
type dshEntry struct {
	ID     string         `yaml:"id"`
	Name   string         `yaml:"name"`
	Config dshEntryConfig `yaml:"config"`
}

type dshEntryConfig struct {
	ServerName string   `yaml:"serverName"`
	Transport  string   `yaml:"transport"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
}

// parseDSHPatch reads the patch layer into nodes; an empty file is an empty
// list, and so is a file that holds only a null.
func parseDSHPatch(src []byte) (*yaml.Node, *yaml.Node, error) {
	if len(bytes.TrimSpace(src)) == 0 {
		root := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, root, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, nil, fmt.Errorf("not valid YAML: %s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("not a list of patches")
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		root.Kind, root.Tag, root.Value, root.Style = yaml.SequenceNode, "!!seq", "", 0
	}
	if root.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("not a list of patches")
	}
	return &doc, root, nil
}

// insertList returns the list of entries an item inserts, or nil for an item
// that inserts nothing.
func insertList(item *yaml.Node) *yaml.Node {
	if item == nil || item.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value == "insert" && item.Content[i+1].Kind == yaml.SequenceNode {
			return item.Content[i+1]
		}
	}
	return nil
}

// dshID is the id of an inserted entry.
func dshID(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "id" && n.Content[i+1].Kind == yaml.ScalarNode {
			return n.Content[i+1].Value
		}
	}
	return ""
}

// dshItem finds Pinky's entry in the patch layer, and the item that inserts
// it.
func dshItem(root *yaml.Node) (item, holder *yaml.Node) {
	for _, it := range root.Content {
		list := insertList(it)
		if list == nil {
			continue
		}
		for _, e := range list.Content {
			if dshID(e) == dshPatchID {
				return e, it
			}
		}
	}
	return nil, nil
}

// readDSHPatch returns Pinky's entry, or nil when there is none.
func readDSHPatch(src []byte) (any, error) {
	_, root, err := parseDSHPatch(src)
	if err != nil {
		return nil, err
	}
	if item, _ := dshItem(root); item != nil {
		return dshEntryOf(item)
	}
	return nil, nil
}

// dshEntryOf decodes an entry, with the command lifted to the top: it sits in
// the plugin's config, while commandOf reads it from the entry.
func dshEntryOf(n *yaml.Node) (map[string]any, error) {
	var m map[string]any
	if err := n.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	cfg, _ := m["config"].(map[string]any)
	if _, ok := m["command"]; !ok {
		m["command"] = cfg["command"]
	}
	if _, ok := m["args"]; !ok {
		m["args"] = cfg["args"]
	}
	return m, nil
}

// dshEntryNode builds Pinky's entry for srv.
func dshEntryNode(srv Server) (*yaml.Node, error) {
	data, err := yaml.Marshal(dshEntry{
		ID:   dshPatchID,
		Name: dshPlugin,
		Config: dshEntryConfig{
			ServerName: Name,
			Transport:  "stdio",
			Command:    srv.Command,
			Args:       srv.Args,
		},
	})
	if err != nil {
		return nil, err
	}
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return n.Content[0], nil
	}
	return &n, nil
}

// marshalDSHPatch writes the patch layer back as YAML, indented as the file
// was (the encoder lays the whole file out again).
func marshalDSHPatch(doc *yaml.Node, indent int) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(indent)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// dshIndent is how deep the file indents one level: the smallest indentation
// of its indented lines, or four spaces (DeepSeek Harness's own files) when
// there is none or it is unreasonable.
func dshIndent(src []byte) int {
	step := 0
	for _, line := range bytes.Split(src, []byte("\n")) {
		trimmed := bytes.TrimLeft(line, " ")
		n := len(line) - len(trimmed)
		if n == 0 || len(trimmed) == 0 {
			continue
		}
		if step == 0 || n < step {
			step = n
		}
	}
	if step < 2 || step > 8 {
		return 4
	}
	return step
}

// setDSHPatch returns the patch layer with Pinky's entry set right.
func setDSHPatch(data []byte, srv Server) ([]byte, error) {
	doc, root, err := parseDSHPatch(data)
	if err != nil {
		return nil, err
	}
	entry, err := dshEntryNode(srv)
	if err != nil {
		return nil, err
	}
	if item, _ := dshItem(root); item != nil {
		// An entry that already says this is left as it is, comments and
		// all, the way JSON and TOML leave theirs.
		var have, want any
		if err := item.Decode(&have); err != nil {
			return nil, err
		}
		if err := entry.Decode(&want); err != nil {
			return nil, err
		}
		if reflect.DeepEqual(have, want) {
			return data, nil
		}
		// The entry's own comments stay; the rest is this Pinky's.
		item.Kind, item.Tag, item.Value, item.Style, item.Content = entry.Kind, entry.Tag, entry.Value, entry.Style, entry.Content
	} else {
		root.Content = append(root.Content, &yaml.Node{
			Kind: yaml.MappingNode, Tag: "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "insert"},
				{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{entry}},
			},
		})
	}
	out, err := marshalDSHPatch(doc, dshIndent(data))
	if err != nil {
		return nil, err
	}
	if err := checkDSHPatch(data, out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// deleteDSHPatch returns the patch layer without Pinky's entry, and whether
// there was one.
func deleteDSHPatch(data []byte) ([]byte, bool, error) {
	doc, root, err := parseDSHPatch(data)
	if err != nil {
		return nil, false, err
	}
	item, holder := dshItem(root)
	if item == nil {
		return data, false, nil
	}
	list := insertList(holder)
	for i, e := range list.Content {
		if e == item {
			list.Content = append(list.Content[:i], list.Content[i+1:]...)
			break
		}
	}
	if len(list.Content) == 0 {
		for i, it := range root.Content {
			if it == holder {
				root.Content = append(root.Content[:i], root.Content[i+1:]...)
				break
			}
		}
	}
	if len(root.Content) == 0 && len(bytes.TrimSpace(data)) == 0 {
		return data, true, nil
	}
	out, err := marshalDSHPatch(doc, dshIndent(data))
	if err != nil {
		return nil, false, err
	}
	if err := checkDSHPatch(data, out, false); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// dshOthers reads every entry of the patch layer but Pinky's, for the check
// that a change left them as they were.
func dshOthers(src []byte) ([]any, error) {
	_, root, err := parseDSHPatch(src)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, it := range root.Content {
		if list := insertList(it); list != nil {
			for _, e := range list.Content {
				if dshID(e) == dshPatchID {
					continue
				}
				var v any
				if err := e.Decode(&v); err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			continue
		}
		var v any
		if err := it.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// checkDSHPatch reads a change back: what the file holds now is Pinky's entry
// (with want), or nothing (without), and every other entry reads as it did.
func checkDSHPatch(before, after []byte, want bool) error {
	wantOthers, err := dshOthers(before)
	if err != nil {
		return err
	}
	gotOthers, err := dshOthers(after)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(wantOthers, gotOthers) {
		return fmt.Errorf("the change would alter another patch")
	}
	entry, err := readDSHPatch(after)
	if err != nil {
		return err
	}
	if (entry != nil) != want {
		return errNoEntry
	}
	return nil
}
