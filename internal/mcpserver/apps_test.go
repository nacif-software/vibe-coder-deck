package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP Apps (SEP-1865, io.modelcontextprotocol/ui): show_pad points at a
// ui:// resource that hosts render as an interactive view.

func TestShowPadLinksTheView(t *testing.T) {
	session, _, _ := newSession(t)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "show_pad" {
			continue
		}
		ui, _ := tool.Meta["ui"].(map[string]any)
		if ui["resourceUri"] != padViewURI {
			t.Fatalf("show_pad _meta = %v", tool.Meta)
		}
		return
	}
	t.Fatal("no show_pad tool")
}

func TestPadViewResource(t *testing.T) {
	session, _, _ := newSession(t)
	res, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: padViewURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %+v", res.Contents)
	}
	c := res.Contents[0]
	if c.URI != padViewURI || c.MIMEType != "text/html;profile=mcp-app" {
		t.Fatalf("uri %q mime %q", c.URI, c.MIMEType)
	}
	if !strings.HasPrefix(strings.ToLower(c.Text), "<!doctype html>") || !strings.Contains(c.Text, "ui/initialize") {
		t.Fatalf("not the view: %.200s", c.Text)
	}
	ui, _ := c.Meta["ui"].(map[string]any)
	if ui["prefersBorder"] != false {
		t.Fatalf("resource _meta = %v", c.Meta)
	}
}

func TestShowPadReturnsKeysAndText(t *testing.T) {
	session, _, _ := newSession(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show_pad", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out struct {
		Keys []map[string]any `json:"keys"`
	}
	json.Unmarshal(raw, &out)
	if len(out.Keys) != 9 {
		t.Fatalf("structured keys = %d: %s", len(out.Keys), raw)
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "key1") || !strings.Contains(text.Text, "knob-right") {
		t.Fatalf("text fallback = %+v", res.Content)
	}
}

func TestPadViewHasTheIconDrawings(t *testing.T) {
	if strings.Contains(padViewHTML, "/*ICONS*/[]") || !strings.Contains(padViewHTML, `"name":"skull"`) {
		t.Fatal("the icon drawings were not inlined into the view")
	}
}

// The view and the agent need to know which profile the keys belong to, and
// which one is in use, to offer the others.

func TestListKeysNamesTheProfile(t *testing.T) {
	s, store, _ := newSession(t)
	persistent, _ := store.PersistentProfile()
	_, out := call(t, s, "list_keys", map[string]any{})
	if int64(out["profile_id"].(float64)) != persistent {
		t.Fatalf("profile_id = %v, want the persistent %d", out["profile_id"], persistent)
	}
	app, _ := store.AddApp("com.apple.Safari", "Safari", "")
	_, out = call(t, s, "list_keys", map[string]any{"profile_id": app.ActiveProfileID})
	if int64(out["profile_id"].(float64)) != app.ActiveProfileID {
		t.Fatalf("profile_id = %v, want %d", out["profile_id"], app.ActiveProfileID)
	}
}

func TestListProfilesReportsTheOneInUse(t *testing.T) {
	var active int64
	s, store, _ := newSessionWith(t, func(d *Deps) { d.Active = func() int64 { return active } })
	app, _ := store.AddApp("com.apple.Safari", "Safari", "")
	active = app.ActiveProfileID
	_, out := call(t, s, "list_profiles", map[string]any{})
	if int64(out["active_profile_id"].(float64)) != active {
		t.Fatalf("active_profile_id = %v, want %d", out["active_profile_id"], active)
	}
	// Without an Active func (the stdio server with the app closed): the persistent profile.
	s2, store2, _ := newSession(t)
	persistent, _ := store2.PersistentProfile()
	_, out = call(t, s2, "list_profiles", map[string]any{})
	if int64(out["active_profile_id"].(float64)) != persistent {
		t.Fatalf("active_profile_id = %v, want the persistent %d", out["active_profile_id"], persistent)
	}
}

func TestShowPadNamesTheProfile(t *testing.T) {
	s, store, _ := newSession(t)
	app, _ := store.AddApp("com.apple.Safari", "Safari", "")
	res, out := call(t, s, "show_pad", map[string]any{"profile_id": app.ActiveProfileID})
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil || !strings.HasPrefix(text.Text, "Profile: Safari: Default\n") {
		t.Fatalf("text = %+v", res.Content)
	}
	if int64(out["profile_id"].(float64)) != app.ActiveProfileID {
		t.Fatalf("profile_id = %v", out["profile_id"])
	}
}

func TestPadViewOffersTheProfiles(t *testing.T) {
	for _, want := range []string{`id="profile"`, "list_profiles", "profile_id"} {
		if !strings.Contains(padViewHTML, want) {
			t.Fatalf("the view lacks %s", want)
		}
	}
}
