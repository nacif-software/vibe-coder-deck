package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeJSONServersKeepsOtherEntries(t *testing.T) {
	existing := []byte(`{"theme":"dark","mcpServers":{"other":{"command":"x"}}}`)
	out, err := MergeJSONServers(existing, "vibe-coder-deck", map[string]any{"command": "/bin/deck", "args": []string{"mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(out, &doc)
	servers := doc["mcpServers"].(map[string]any)
	if doc["theme"] != "dark" || servers["other"] == nil || servers["vibe-coder-deck"].(map[string]any)["command"] != "/bin/deck" {
		t.Fatalf("merged = %s", out)
	}
	if out, _ = MergeJSONServers(nil, "vibe-coder-deck", map[string]any{"url": "http://x/mcp"}); !strings.Contains(string(out), `"url"`) {
		t.Fatalf("empty file should become a fresh config: %s", out)
	}
	if _, err := MergeJSONServers([]byte(`not json`), "x", nil); err == nil {
		t.Fatal("broken json must not be overwritten")
	}
}

func TestMergeCodexConfigReplacesOnlyOurTable(t *testing.T) {
	existing := "model = \"gpt\"\n\n[mcp_servers.vibe-coder-deck]\ncommand = \"/old\"\nargs = [\"mcp\"]\n\n[mcp_servers.other]\ncommand = \"keep\"\n"
	out := MergeCodexConfig(existing, "vibe-coder-deck", "/new/deck", []string{"mcp"})
	if !strings.Contains(out, `model = "gpt"`) || !strings.Contains(out, `command = "keep"`) || strings.Contains(out, "/old") || !strings.Contains(out, `command = "/new/deck"`) {
		t.Fatalf("merged = %q", out)
	}
}

func TestInstallWritesEachClientConfig(t *testing.T) {
	home := t.TempDir()
	inst := Installer{Executable: "/Applications/Deck.app/Contents/MacOS/vibe-coder-deck", URL: "http://127.0.0.1:7071/mcp", Home: home, ConfigDir: filepath.Join(home, "cfg"),
		RunClaude: func(args ...string) (string, error) { return "ok " + strings.Join(args, " "), nil }}

	for _, client := range []string{"claude-desktop", "cursor", "windsurf", "codex"} {
		res, err := inst.Install(client)
		if err != nil {
			t.Fatalf("%s: %v", client, err)
		}
		if _, err := os.Stat(res.Path); err != nil {
			t.Fatalf("%s: config not written at %s", client, res.Path)
		}
	}
	res, err := inst.Install("claude-code")
	if err != nil || !strings.Contains(res.Message, "mcp add") || !strings.Contains(res.Message, "vibe-coder-deck") {
		t.Fatalf("claude-code = %+v, %v", res, err)
	}
	if _, err := inst.Install("emacs"); err == nil {
		t.Fatal("unknown client must fail")
	}
	cursor, _ := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if !strings.Contains(string(cursor), inst.Executable) {
		t.Fatalf("cursor config = %s", cursor)
	}
	if len(Clients()) < 5 {
		t.Fatalf("clients = %v", Clients())
	}
}
