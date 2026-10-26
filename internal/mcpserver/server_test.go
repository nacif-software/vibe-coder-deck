package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

func newSession(t *testing.T) (*mcp.ClientSession, *deck.Store, *[]deck.Slot) {
	t.Helper()
	return newSessionWith(t, nil)
}

// newSessionWith lets a test adjust the Deps (an Active func, say) before the server starts.
func newSessionWith(t *testing.T, adjust func(*Deps)) (*mcp.ClientSession, *deck.Store, *[]deck.Slot) {
	t.Helper()
	store, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	pressed := &[]deck.Slot{}
	deps := Deps{
		Store:     store,
		Modifiers: "ctrl+opt+cmd",
		Sync: func(dryRun bool) (deck.Plan, error) {
			if !dryRun {
				store.MarkSynced(store.Now())
			}
			return deck.Plan{{Slot: deck.Key1, Action: "ctrl+opt+cmd+1"}}, nil
		},
		Press: func(slot deck.Slot) (deck.Outcome, error) {
			*pressed = append(*pressed, slot)
			return deck.Outcome{Slot: slot, Ran: true, ExitCode: 0, Output: "ok"}, nil
		},
		Version: "test",
	}
	if adjust != nil {
		adjust(&deps)
	}
	server := New(deps)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, st)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, store, pressed
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out map[string]any
	if res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(raw, &out)
	}
	return res, out
}

func TestToolsAreListed(t *testing.T) {
	s, _, _ := newSession(t)
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"list_keys", "get_key", "set_key", "sync_pad", "press_key", "list_runs"} {
		if !got[want] {
			t.Errorf("tool %s missing from %v", want, got)
		}
	}
}

func TestListKeysDescribesThePad(t *testing.T) {
	s, _, _ := newSession(t)
	res, out := call(t, s, "list_keys", nil)
	if res.IsError {
		t.Fatalf("error: %v", res.Content)
	}
	keys := out["keys"].([]any)
	if len(keys) != 9 || keys[0].(map[string]any)["slot"] != "key1" || keys[8].(map[string]any)["hotkey"] != "ctrl+opt+cmd+9" {
		t.Fatalf("keys = %v", keys)
	}
	if out["hotkey_modifiers"] != "ctrl+opt+cmd" || len(out["modes"].([]any)) != 5 {
		t.Fatalf("out = %v", out)
	}
}

func TestSetKeyThenGetKey(t *testing.T) {
	s, store, _ := newSession(t)
	res, out := call(t, s, "set_key", map[string]any{
		"slot": "key1", "label": "Handoff", "mode": "script", "action": "/h.sh",
		"app_scope": "Orca", "cooldown_seconds": 60, "single_instance": true,
	})
	if res.IsError {
		t.Fatalf("error: %v", res.Content)
	}
	if out["key"].(map[string]any)["mode"] != "script" {
		t.Fatalf("out = %v", out)
	}
	k, _ := store.Key(deck.Key1)
	if k.Mode != deck.ModeScript || k.Label != "Handoff" || k.AppScope != "Orca" || k.CooldownSeconds != 60 {
		t.Fatalf("saved = %+v", k)
	}

	// A later call that only changes the label leaves the rest alone.
	call(t, s, "set_key", map[string]any{"slot": "key1", "label": "Handoff!"})
	k, _ = store.Key(deck.Key1)
	if k.Label != "Handoff!" || k.Mode != deck.ModeScript || k.Action != "/h.sh" {
		t.Fatalf("partial update broke the key: %+v", k)
	}

	_, out = call(t, s, "get_key", map[string]any{"slot": "key1"})
	if out["key"].(map[string]any)["label"] != "Handoff!" {
		t.Fatalf("get_key = %v", out)
	}
}

func TestSetKeyMacroSteps(t *testing.T) {
	s, store, _ := newSession(t)
	res, _ := call(t, s, "set_key", map[string]any{
		"slot": "knob-press", "mode": "macro",
		"steps": []map[string]any{{"type": "keys", "value": "cmd+a"}, {"type": "delay", "value": 200}},
	})
	if res.IsError {
		t.Fatalf("error: %v", res.Content)
	}
	k, _ := store.Key(deck.KnobPress)
	if len(k.Steps) != 2 || k.Steps[1].Value != "200" {
		t.Fatalf("steps = %+v", k.Steps)
	}
}

func TestSetKeyReportsValidationAsToolError(t *testing.T) {
	s, _, _ := newSession(t)
	res, _ := call(t, s, "set_key", map[string]any{"slot": "key1", "mode": "script"})
	if !res.IsError {
		t.Fatal("script without action should be a tool error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "action") {
		t.Fatalf("error text = %q", text)
	}
	if res, _ := call(t, s, "set_key", map[string]any{"slot": "key9", "mode": "off"}); !res.IsError {
		t.Fatal("unknown slot should be a tool error")
	}
}

func TestSyncPad(t *testing.T) {
	s, store, _ := newSession(t)
	_, out := call(t, s, "sync_pad", map[string]any{"dry_run": true})
	if out["written"] != false || len(out["plan"].([]any)) != 1 {
		t.Fatalf("dry run = %v", out)
	}
	_, out = call(t, s, "sync_pad", nil)
	if out["written"] != true {
		t.Fatalf("sync = %v", out)
	}
	if _, ok, _ := store.SyncedAt(); !ok {
		t.Fatal("sync should have marked the store")
	}
}

func TestPressKeyAndRuns(t *testing.T) {
	s, store, pressed := newSession(t)
	store.SaveKey(deck.Key{Slot: deck.Key1, Mode: deck.ModeShell, Action: "echo"})
	_, out := call(t, s, "press_key", map[string]any{"slot": "key1"})
	if out["ran"] != true || len(*pressed) != 1 || (*pressed)[0] != deck.Key1 {
		t.Fatalf("press = %v, pressed %v", out, *pressed)
	}
	run, _ := store.CreateRun(deck.Key1)
	zero := 0
	store.FinishRun(run.ID, "ok", &zero, "done")
	_, out = call(t, s, "list_runs", map[string]any{"slot": "key1"})
	runs := out["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["status"] != "ok" {
		t.Fatalf("runs = %v", runs)
	}
}

func TestHTTPHandlerSpeaksStreamableHTTP(t *testing.T) {
	store, _ := deck.Open(":memory:")
	t.Cleanup(func() { store.Close() })
	h := Handler(Deps{Store: store, Modifiers: "ctrl+opt+cmd", Version: "test",
		Sync:  func(bool) (deck.Plan, error) { return nil, nil },
		Press: func(deck.Slot) (deck.Outcome, error) { return deck.Outcome{}, nil }})
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	req := httptest.NewRequest("POST", "http://127.0.0.1:7071/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "vibe-coder-deck") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

// The 2026-07-28 revision ("MCP v2") is stateless: no initialize handshake, a
// server/discover call instead. Both transports must negotiate it.
const statelessRevision = "2026-07-28"

func TestNegotiatesStatelessRevisionInMemory(t *testing.T) {
	session, _, _ := newSession(t)
	if v := session.InitializeResult().ProtocolVersion; v != statelessRevision {
		t.Fatalf("protocol version = %q, want %q", v, statelessRevision)
	}
	call(t, session, "list_keys", nil)
}

func TestNegotiatesStatelessRevisionOverHTTP(t *testing.T) {
	store, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(Handler(Deps{Store: store, Modifiers: "ctrl+opt+cmd", Version: "test"}))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	if v := session.InitializeResult().ProtocolVersion; v != statelessRevision {
		t.Fatalf("protocol version = %q, want %q", v, statelessRevision)
	}
	call(t, session, "list_keys", nil)
}

func TestOlderClientsStillConnect(t *testing.T) {
	store, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(Handler(Deps{Store: store, Modifiers: "ctrl+opt+cmd", Version: "test"}))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-06-18"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	if v := session.InitializeResult().ProtocolVersion; v != "2025-06-18" {
		t.Fatalf("protocol version = %q, want 2025-06-18", v)
	}
	call(t, session, "list_keys", nil)
}

func TestMacroTools(t *testing.T) {
	session, store, _ := newSession(t)
	_, out := call(t, session, "set_macro", map[string]any{"name": "Spam", "kind": "repeat", "steps": []any{map[string]any{"type": "keys", "value": "space"}}})
	id := int64(out["macro"].(map[string]any)["id"].(float64))
	if id == 0 {
		t.Fatalf("set_macro = %v", out)
	}
	call(t, session, "set_key", map[string]any{"slot": "key4", "mode": "macro", "macro_id": id})
	if k, _ := store.Key("key4"); k.MacroID != id {
		t.Fatalf("key4 = %+v", k)
	}
	_, out = call(t, session, "list_macros", map[string]any{})
	list := out["macros"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["assigned_to"].([]any)[0] != "key4" {
		t.Fatalf("list_macros = %v", out)
	}
	call(t, session, "set_macro", map[string]any{"id": id, "name": "Spam", "kind": "toggle", "steps": []any{map[string]any{"type": "keys", "value": "space"}}})
	if m, _ := store.Macro(id); m.Kind != "toggle" {
		t.Fatalf("kind = %s", m.Kind)
	}
	call(t, session, "delete_macro", map[string]any{"id": id})
	if k, _ := store.Key("key4"); k.Mode != "off" {
		t.Fatalf("key4 after delete = %+v", k)
	}
}
