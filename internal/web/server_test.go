package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
	"github.com/nacif-software/vibe-coder-deck/internal/deck"
	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

func newServer(t *testing.T) (http.Handler, *deck.Store, *int) {
	t.Helper()
	s, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	synced := 0
	h := New(s, "ctrl+opt+cmd", func(dryRun bool) (deck.Plan, error) {
		if !dryRun {
			synced++
		}
		return deck.Plan{{Slot: "key1", Action: "ctrl+opt+cmd+1"}}, nil
	})
	return h, s, &synced
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestKeysEndpointListsNineSlots(t *testing.T) {
	h, _, _ := newServer(t)
	rec := do(t, h, "GET", "/api/keys", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Keys            []map[string]any `json:"keys"`
		Modes           []map[string]any `json:"modes"`
		HotkeyModifiers string           `json:"hotkey_modifiers"`
		SyncedAt        *string          `json:"synced_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Keys) != 9 || body.Keys[0]["slot"] != "key1" || body.Keys[0]["hotkey"] != "ctrl+opt+cmd+1" || body.Keys[8]["slot"] != "knob-right" {
		t.Fatalf("keys = %v", body.Keys)
	}
	if len(body.Modes) != 5 || body.HotkeyModifiers != "ctrl+opt+cmd" || body.SyncedAt != nil {
		t.Fatalf("body = %+v", body)
	}
}

func TestUpdateKey(t *testing.T) {
	h, s, _ := newServer(t)
	rec := do(t, h, "PUT", "/api/keys/key1", `{"label":"Handoff","mode":"script","action":"/h.sh","steps":[],"app_scope":"Orca","cooldown_seconds":60,"single_instance":true}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	k, _ := s.Key(deck.Key1)
	if k.Mode != deck.ModeScript || k.Label != "Handoff" || k.AppScope != "Orca" || k.CooldownSeconds != 60 || !k.SingleInstance {
		t.Fatalf("saved = %+v", k)
	}
}

func TestUpdateKeyValidates(t *testing.T) {
	h, _, _ := newServer(t)
	if rec := do(t, h, "PUT", "/api/keys/key1", `{"mode":"script","action":""}`); rec.Code != 422 || !strings.Contains(rec.Body.String(), "action") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/keys/key1", `{"mode":"macro","steps":[{"type":"teleport","value":"x"}]}`); rec.Code != 422 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/keys/key9", `{"mode":"off"}`); rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSyncEndpoint(t *testing.T) {
	h, _, synced := newServer(t)
	rec := do(t, h, "POST", "/api/sync", "")
	if rec.Code != 200 || *synced != 1 || !strings.Contains(rec.Body.String(), "ctrl+opt+cmd+1") {
		t.Fatalf("status %d synced %d body %s", rec.Code, *synced, rec.Body)
	}
}

func TestIndexServesThePage(t *testing.T) {
	h, _, _ := newServer(t)
	rec := do(t, h, "GET", "/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Vibe Coder Deck") {
		t.Fatalf("status %d", rec.Code)
	}
}

type fakePad struct {
	status  PadStatus
	presses []PadPress
}

func (f *fakePad) Status() PadStatus { return f.status }

func (f *fakePad) Presses(since int) ([]PadPress, int) {
	if since > len(f.presses) {
		since = len(f.presses)
	}
	return f.presses[since:], len(f.presses)
}

func TestPadEndpointTracesPressesToSlots(t *testing.T) {
	h, s, _ := newServer(t)
	s.SaveKey(deck.Key{Slot: deck.Key2, Mode: deck.ModeKeys, Action: "cmd+shift+4", SingleInstance: true})
	h.(*Server).Pad = &fakePad{
		status:  PadStatus{Connected: true, Product: "Vibe Coder", VendorID: 0x1189, ProductID: 0x8890, InputAllowed: true},
		presses: []PadPress{{Combo: "ctrl+opt+cmd+1", At: 1}, {Combo: "shift+cmd+4", At: 2}},
	}
	rec := do(t, h, "GET", "/api/pad?since=1", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Connected bool   `json:"connected"`
		VendorID  int    `json:"vendor_id"`
		Product   string `json:"product"`
		Next      int    `json:"next"`
		Presses   []struct {
			Combo string   `json:"combo"`
			Slots []string `json:"slots"`
		} `json:"presses"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Connected || body.VendorID != 0x1189 || body.Product != "Vibe Coder" || body.Next != 2 {
		t.Fatalf("body = %s", rec.Body)
	}
	if len(body.Presses) != 1 || body.Presses[0].Combo != "shift+cmd+4" || len(body.Presses[0].Slots) != 1 || body.Presses[0].Slots[0] != "key2" {
		t.Fatalf("presses = %+v", body.Presses)
	}
}

func TestPadEndpointWithoutWatcher(t *testing.T) {
	h, _, _ := newServer(t)
	rec := do(t, h, "GET", "/api/pad", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"connected":false`) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

type fakePermissions struct{ requested []string }

func (f *fakePermissions) List() []Permission {
	return []Permission{{Name: "accessibility", Label: "Accessibility", Granted: true}, {Name: "input-monitoring", Label: "Input Monitoring"}}
}

func (f *fakePermissions) Request(name string) error {
	f.requested = append(f.requested, name)
	return nil
}

func TestPermissionsEndpoints(t *testing.T) {
	h, _, _ := newServer(t)
	perms := &fakePermissions{}
	h.(*Server).Permissions = perms
	rec := do(t, h, "GET", "/api/permissions", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"input-monitoring"`) || !strings.Contains(rec.Body.String(), `"granted":false`) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/permissions/input-monitoring", ""); rec.Code != 200 || len(perms.requested) != 1 || perms.requested[0] != "input-monitoring" {
		t.Fatalf("status %d requested %v: %s", rec.Code, perms.requested, rec.Body)
	}
}

func TestDeleteKeyTurnsItOff(t *testing.T) {
	h, s, _ := newServer(t)
	s.SaveKey(deck.Key{Slot: deck.Key4, Label: "Copy", Mode: deck.ModeMacro, Steps: []deck.Step{{Type: "keys", Value: "cmd+c"}}, SingleInstance: true})
	// What the page's Delete button sends.
	rec := do(t, h, "PUT", "/api/keys/key4", `{"label":"","mode":"off","action":"","steps":[],"app_scope":"","cooldown_seconds":0,"single_instance":true}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	k, _ := s.Key(deck.Key4)
	if k.Mode != deck.ModeOff || k.Label != "" || len(k.Steps) != 0 {
		t.Fatalf("after delete = %+v", k)
	}
}

func TestRestartEndpoint(t *testing.T) {
	h, _, _ := newServer(t)
	if rec := do(t, h, "POST", "/api/app/restart", ""); rec.Code != 501 {
		t.Fatalf("without the app: status %d", rec.Code)
	}
	restarted := 0
	h.(*Server).Restart = func() error { restarted++; return nil }
	if rec := do(t, h, "GET", "/api/app", ""); !strings.Contains(rec.Body.String(), `"can_restart":true`) {
		t.Fatalf("app info: %s", rec.Body)
	}
	if rec := do(t, h, "POST", "/api/app/restart", ""); rec.Code != 200 || restarted != 1 {
		t.Fatalf("status %d restarted %d", rec.Code, restarted)
	}
}

func TestLoginItemEndpoint(t *testing.T) {
	h, _, _ := newServer(t)
	if rec := do(t, h, "GET", "/api/app", ""); !strings.Contains(rec.Body.String(), `"login_item":null`) {
		t.Fatalf("without the app: %s", rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/app/login", `{"on":true}`); rec.Code != 501 {
		t.Fatalf("without the app: status %d", rec.Code)
	}
	on := false
	h.(*Server).LoginItem = &LoginItem{Enabled: func() bool { return on }, Set: func(v bool) error { on = v; return nil }}
	if rec := do(t, h, "GET", "/api/app", ""); !strings.Contains(rec.Body.String(), `"login_item":false`) {
		t.Fatalf("app info: %s", rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/app/login", `{"on":true}`); rec.Code != 200 || !on || !strings.Contains(rec.Body.String(), `"login_item":true`) {
		t.Fatalf("status %d on %v: %s", rec.Code, on, rec.Body)
	}
	h.(*Server).LoginItem.Set = func(bool) error { return errors.New("launchctl failed") }
	if rec := do(t, h, "PUT", "/api/app/login", `{"on":false}`); rec.Code != 500 || !strings.Contains(rec.Body.String(), "launchctl failed") {
		t.Fatalf("failure: status %d %s", rec.Code, rec.Body)
	}
}

func TestSaveWritesThePad(t *testing.T) {
	h, _, synced := newServer(t)
	rec := do(t, h, "PUT", "/api/keys/key2", `{"label":"Shot","mode":"keys","action":"cmd+shift+4","steps":[],"single_instance":true}`)
	if rec.Code != 200 || *synced != 1 || !strings.Contains(rec.Body.String(), `"synced":true`) {
		t.Fatalf("status %d synced %d: %s", rec.Code, *synced, rec.Body)
	}
}

func TestSaveKeepsTheKeyWhenThePadIsUnplugged(t *testing.T) {
	s, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := New(s, "ctrl+opt+cmd", func(dryRun bool) (deck.Plan, error) { return nil, pad.ErrNotConnected })
	rec := do(t, h, "PUT", "/api/keys/key2", `{"label":"Shot","mode":"keys","action":"cmd+shift+4","steps":[],"single_instance":true}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"synced":false`) || !strings.Contains(rec.Body.String(), "not connected") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if k, _ := s.Key(deck.Key2); k.Label != "Shot" {
		t.Fatalf("key not saved: %+v", k)
	}
}

func TestLogsEndpointTailsTheFile(t *testing.T) {
	h, _, _ := newServer(t)
	path := t.TempDir() + "/deck.log"
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	h.(*Server).LogPath = path
	rec := do(t, h, "GET", "/api/logs?lines=5", "")
	var body struct {
		Path  string   `json:"path"`
		Lines []string `json:"lines"`
		Debug bool     `json:"debug"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 200 || body.Path != path || len(body.Lines) != 5 || body.Lines[0] != "line 296" || body.Lines[4] != "line 300" {
		t.Fatalf("status %d: %+v", rec.Code, body)
	}
}

func TestDebugEndpointSavesTheSetting(t *testing.T) {
	h, s, _ := newServer(t)
	t.Cleanup(func() { debuglog.Set(false) })
	if rec := do(t, h, "PUT", "/api/debug", `{"on":true}`); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if on, _ := s.Debug(); !on || !debuglog.Enabled() {
		t.Fatalf("store %v, debuglog %v", on, debuglog.Enabled())
	}
	if rec := do(t, h, "GET", "/api/logs", ""); !strings.Contains(rec.Body.String(), `"debug":true`) {
		t.Fatalf("logs: %s", rec.Body)
	}
}

func TestKeyIcon(t *testing.T) {
	h, s, _ := newServer(t)
	rec := do(t, h, "PUT", "/api/keys/key2", `{"label":"Play","icon":"icon:play","mode":"keys","action":"play","steps":[]}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if k, _ := s.Key(deck.Key2); k.Icon != "icon:play" {
		t.Fatalf("icon = %q", k.Icon)
	}
	// Without the field the icon stays (Delete keeps the keycap's print).
	do(t, h, "PUT", "/api/keys/key2", `{"label":"","mode":"off","action":"","steps":[]}`)
	if k, _ := s.Key(deck.Key2); k.Icon != "icon:play" {
		t.Fatalf("icon after a save without it = %q", k.Icon)
	}
	if rec := do(t, h, "PUT", "/api/keys/key2", `{"icon":"icon:teleport","mode":"off"}`); rec.Code != 422 {
		t.Fatalf("bad icon: status %d", rec.Code)
	}
	rec = do(t, h, "GET", "/api/keys", "")
	if !strings.Contains(rec.Body.String(), `"icons":[{"name":"skull"`) {
		t.Fatalf("keys has no icon list: %.300s", rec.Body)
	}
}

func TestMacroLibraryEndpoints(t *testing.T) {
	h, s, _ := newServer(t)
	rec := do(t, h, "POST", "/api/macros", `{"name":"Copy","kind":"repeat","steps":[{"type":"keys","value":"cmd+c"}],"standard_delay_ms":50,"color":"#ff8800"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created deck.Macro
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == 0 || created.Kind != deck.MacroRepeat {
		t.Fatalf("created = %+v", created)
	}
	if rec := do(t, h, "PUT", fmt.Sprintf("/api/keys/key3"), fmt.Sprintf(`{"label":"","mode":"macro","macro_id":%d}`, created.ID)); rec.Code != 200 {
		t.Fatalf("assign: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/macros", "")
	if !strings.Contains(rec.Body.String(), `"assigned_to":["key3"]`) {
		t.Fatalf("list: %s", rec.Body)
	}
	if rec := do(t, h, "PUT", fmt.Sprintf("/api/macros/%d", created.ID), `{"name":"Copy all","kind":"once","steps":[{"type":"keys","value":"cmd+a"}]}`); rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if m, _ := s.Macro(created.ID); m.Name != "Copy all" || m.Kind != deck.MacroOnce {
		t.Fatalf("updated = %+v", m)
	}
	if rec := do(t, h, "POST", "/api/macros", `{"name":"","kind":"once","steps":[]}`); rec.Code != 422 {
		t.Fatalf("invalid: %d", rec.Code)
	}
	if rec := do(t, h, "DELETE", fmt.Sprintf("/api/macros/%d", created.ID), ""); rec.Code != 200 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if k, _ := s.Key(deck.Key3); k.Mode != deck.ModeOff {
		t.Fatalf("key3 after delete = %+v", k)
	}
	if rec := do(t, h, "DELETE", "/api/macros/999", ""); rec.Code != 404 {
		t.Fatalf("missing: %d", rec.Code)
	}
}

type fakeApps struct{}

func (fakeApps) Installed() []AppInfo {
	return []AppInfo{{Name: "Safari", BundleID: "com.apple.Safari", Path: "/Applications/Safari.app"}}
}
func (fakeApps) Info(path string) (AppInfo, error) {
	return AppInfo{Name: "Safari", BundleID: "com.apple.Safari", Path: path}, nil
}
func (fakeApps) Choose() (AppInfo, bool, error)   { return AppInfo{}, false, nil }
func (fakeApps) Icon(path string) ([]byte, error) { return []byte("png"), nil }

func TestProfilesFlow(t *testing.T) {
	h, s, synced := newServer(t)
	h.(*Server).Apps = fakeApps{}
	rec := do(t, h, "POST", "/api/apps", `{"path":"/Applications/Safari.app"}`)
	if rec.Code != 201 {
		t.Fatalf("add app: %d %s", rec.Code, rec.Body)
	}
	var app deck.App
	json.Unmarshal(rec.Body.Bytes(), &app)
	if app.BundleID != "com.apple.Safari" || app.ActiveProfileID == 0 {
		t.Fatalf("app = %+v", app)
	}
	// Keys of that profile, apart from the persistent one.
	rec = do(t, h, "PUT", fmt.Sprintf("/api/keys/key2?profile=%d", app.ActiveProfileID), `{"mode":"keys","action":"cmd+t"}`)
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if k, _ := s.KeyIn(app.ActiveProfileID, deck.Key2); k.Action != "cmd+t" {
		t.Fatalf("safari key2 = %+v", k)
	}
	if k, _ := s.Key(deck.Key2); k.Action == "cmd+t" {
		t.Fatal("the persistent profile changed too")
	}
	before := *synced
	if rec := do(t, h, "PUT", "/api/profiles/settings", `{"switching":true}`); rec.Code != 200 || *synced != before+1 {
		t.Fatalf("settings: %d %s (synced %d)", rec.Code, rec.Body, *synced)
	}
	rec = do(t, h, "GET", "/api/profiles", "")
	if !strings.Contains(rec.Body.String(), `"switching":true`) || !strings.Contains(rec.Body.String(), `"name":"Safari"`) {
		t.Fatalf("profiles: %s", rec.Body)
	}
	if rec := do(t, h, "GET", "/api/app-icon?path=/Applications/Safari.app", ""); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon: %d", rec.Code)
	}
}

func TestAddAppByNameUsesTheInstalledApp(t *testing.T) {
	h, _, _ := newServer(t)
	h.(*Server).Apps = fakeApps{}
	rec := do(t, h, "POST", "/api/apps", `{"name":"safari"}`)
	if rec.Code != 201 {
		t.Fatalf("add by name: %d %s", rec.Code, rec.Body)
	}
	var app deck.App
	json.Unmarshal(rec.Body.Bytes(), &app)
	if app.Name != "Safari" || app.BundleID != "com.apple.Safari" || app.Path != "/Applications/Safari.app" {
		t.Fatalf("app = %+v", app)
	}
	// A name no installed app has is kept as typed, to match once seen in front.
	rec = do(t, h, "POST", "/api/apps", `{"name":"Orca"}`)
	json.Unmarshal(rec.Body.Bytes(), &app)
	if rec.Code != 201 || app.Name != "Orca" || app.BundleID != "" || app.Path != "" {
		t.Fatalf("unknown name: %d %+v", rec.Code, app)
	}
}
