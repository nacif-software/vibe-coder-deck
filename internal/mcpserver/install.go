package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client is an AI tool the MCP server can be registered with.
type Client struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note"`
}

// ServerName is how the server is registered everywhere.
const ServerName = "vibe-coder-deck"

func Clients() []Client {
	return []Client{
		{ID: "claude-code", Name: "Claude Code", Note: "runs `claude mcp add` (user scope)"},
		{ID: "claude-desktop", Name: "Claude Desktop", Note: "claude_desktop_config.json"},
		{ID: "cursor", Name: "Cursor", Note: "~/.cursor/mcp.json"},
		{ID: "windsurf", Name: "Windsurf", Note: "~/.codeium/windsurf/mcp_config.json"},
		{ID: "codex", Name: "Codex CLI", Note: "~/.codex/config.toml"},
	}
}

// Installer registers the server with a client's configuration.
type Installer struct {
	Executable string // this program, run with `mcp` for stdio
	URL        string // the Streamable HTTP endpoint of the running app
	Home       string // the user's home directory
	ConfigDir  string // os.UserConfigDir()
	Transport  string // "stdio" (default, works without the app) or "http"
	RunClaude  func(args ...string) (string, error)
}

// Result says what was written where.
type Result struct {
	Client  string `json:"client"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (i Installer) stdioEntry() map[string]any {
	return map[string]any{"command": i.Executable, "args": []string{"mcp"}}
}

func (i Installer) entry() map[string]any {
	if i.Transport == "http" {
		return map[string]any{"url": i.URL}
	}
	return i.stdioEntry()
}

func (i Installer) Install(client string) (Result, error) {
	switch client {
	case "claude-code":
		return i.installClaudeCode()
	case "claude-desktop":
		// Claude Desktop only speaks stdio.
		return i.writeJSON(client, filepath.Join(i.ConfigDir, "Claude", "claude_desktop_config.json"), i.stdioEntry())
	case "cursor":
		return i.writeJSON(client, filepath.Join(i.Home, ".cursor", "mcp.json"), i.entry())
	case "windsurf":
		return i.writeJSON(client, filepath.Join(i.Home, ".codeium", "windsurf", "mcp_config.json"), i.entry())
	case "codex":
		return i.writeCodex(filepath.Join(i.Home, ".codex", "config.toml"))
	default:
		return Result{}, fmt.Errorf("unknown client %q; use one of claude-code, claude-desktop, cursor, windsurf, codex", client)
	}
}

// ClaudeCodeArgs is the `claude mcp add` invocation for this server.
func (i Installer) ClaudeCodeArgs() []string {
	if i.Transport == "http" {
		return []string{"mcp", "add", "-s", "user", "--transport", "http", ServerName, i.URL}
	}
	return []string{"mcp", "add", "-s", "user", ServerName, "--", i.Executable, "mcp"}
}

func (i Installer) installClaudeCode() (Result, error) {
	args := i.ClaudeCodeArgs()
	command := "claude " + strings.Join(args, " ")
	run := i.RunClaude
	if run == nil {
		run = func(args ...string) (string, error) {
			if _, err := exec.LookPath("claude"); err != nil {
				return "", errors.New("the `claude` command is not on PATH")
			}
			out, err := exec.Command("claude", args...).CombinedOutput()
			return strings.TrimSpace(string(out)), err
		}
	}
	out, err := run(args...)
	if err != nil {
		return Result{Client: "claude-code"}, fmt.Errorf("%v; run it yourself: %s", err, command)
	}
	return Result{Client: "claude-code", Message: "registered with Claude Code: " + command + "\n" + out}, nil
}

func (i Installer) writeJSON(client, path string, entry map[string]any) (Result, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	merged, err := MergeJSONServers(existing, ServerName, entry)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := writeFile(path, merged); err != nil {
		return Result{}, err
	}
	return Result{Client: client, Path: path, Message: "written " + path + " (restart the app to pick it up)"}, nil
}

func (i Installer) writeCodex(path string) (Result, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	merged := MergeCodexConfig(string(existing), ServerName, i.Executable, []string{"mcp"})
	if err := writeFile(path, []byte(merged)); err != nil {
		return Result{}, err
	}
	return Result{Client: "codex", Path: path, Message: "written " + path}, nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// MergeJSONServers sets mcpServers[name] = entry in a JSON config, keeping
// everything else. A missing or empty file becomes a fresh config; a file
// that is not JSON is left alone.
func MergeJSONServers(existing []byte, name string, entry map[string]any) ([]byte, error) {
	doc := map[string]any{}
	if trimmed := bytes.TrimSpace(existing); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &doc); err != nil {
			return nil, fmt.Errorf("not valid JSON, not touching it: %w", err)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	doc["mcpServers"] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// MergeCodexConfig replaces (or appends) the [mcp_servers.<name>] table of a
// Codex config.toml, keeping every other line.
func MergeCodexConfig(existing, name, command string, args []string) string {
	header := "[mcp_servers." + name + "]"
	var kept []string
	skipping := false
	for _, line := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(trimmed, "[") {
			skipping = false
		}
		if !skipping {
			kept = append(kept, line)
		}
	}
	body := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, fmt.Sprintf("%q", a))
	}
	block := fmt.Sprintf("%s\ncommand = %q\nargs = [%s]\n", header, command, strings.Join(quoted, ", "))
	if body == "" {
		return block
	}
	return body + "\n\n" + block
}
