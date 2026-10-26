// Package web serves the configuration page and its JSON API.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
	"github.com/nacif-software/vibe-coder-deck/internal/deck"
	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

//go:embed static
var static embed.FS

// SyncFunc writes the configuration into the pad (or only plans it).
type SyncFunc func(dryRun bool) (deck.Plan, error)

// PressFunc runs a slot as if its key had been pressed.
type PressFunc func(slot deck.Slot) (deck.Outcome, error)

// MCPInfo describes the MCP server for the page's "AI setup" section.
type MCPInfo struct {
	URL        string `json:"url"`
	Executable string `json:"executable"`
	Clients    []any  `json:"clients"`
}

// MCPInstallFunc registers the MCP server with a client; it returns a message.
type MCPInstallFunc func(client string) (message string, err error)

// Recorder captures keys and clicks system-wide for the macro editor.
type Recorder interface {
	Start(mouse bool) error
	Stop()
	Events(since int) (recording bool, events []RecordedEvent)
}

// RecordedEvent mirrors platform.RecordedEvent without importing it.
type RecordedEvent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	At    int64  `json:"at"`
}

// PadWatcher reports whether the pad is plugged in and the keystrokes it
// sent, numbered from 0; Presses returns those from since on and the next
// number to ask for.
type PadWatcher interface {
	Status() PadStatus
	Presses(since int) (presses []PadPress, next int)
}

// PadStatus is the pad's USB identity while it is connected. InputAllowed is
// false when the OS hides its keystrokes (no Input Monitoring on macOS).
type PadStatus struct {
	Connected    bool   `json:"connected"`
	Product      string `json:"product"`
	Manufacturer string `json:"manufacturer"`
	VendorID     int    `json:"vendor_id"`
	ProductID    int    `json:"product_id"`
	InputAllowed bool   `json:"input_allowed"`
}

// PadPress is one keystroke the pad sent, in the pad's action syntax.
type PadPress struct {
	Combo string `json:"combo"`
	At    int64  `json:"at"`
}

// Permissions lists what the OS must allow and asks for it.
type Permissions interface {
	List() []Permission
	Request(name string) error
}

type Permission struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Why     string `json:"why"`
	Granted bool   `json:"granted"`
}

type Server struct {
	Recorder    Recorder
	Pad         PadWatcher
	Permissions Permissions
	// Restart quits the app and opens it again; nil outside the desktop app.
	Restart func() error
	Apps    AppsProvider
	Active  ActiveFunc
	// LogPath is the deck's log file, shown in the page; OpenPath opens it.
	LogPath    string
	OpenPath   func(path string) error
	store      *deck.Store
	modifiers  string
	sync       SyncFunc
	Press      PressFunc
	MCP        *MCPInfo
	MCPInstall MCPInstallFunc
	mux        *http.ServeMux
}

func New(store *deck.Store, modifiers string, sync SyncFunc) *Server {
	s := &Server{store: store, modifiers: modifiers, sync: sync, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/keys", s.keys)
	s.mux.HandleFunc("PUT /api/keys/{slot}", s.update)
	s.mux.HandleFunc("GET /api/runs/{slot}", s.runs)
	s.mux.HandleFunc("POST /api/sync", s.doSync)
	s.mux.HandleFunc("POST /api/press/{slot}", s.press)
	s.mux.HandleFunc("GET /api/mcp", s.mcpInfo)
	s.mux.HandleFunc("POST /api/record/start", s.recordStart)
	s.mux.HandleFunc("POST /api/record/stop", s.recordStop)
	s.mux.HandleFunc("GET /api/record/events", s.recordEvents)
	s.mux.HandleFunc("POST /api/mcp/install", s.mcpInstall)
	s.mux.HandleFunc("GET /api/pad", s.padStatus)
	s.mux.HandleFunc("GET /api/permissions", s.permissions)
	s.mux.HandleFunc("POST /api/permissions/{name}", s.requestPermission)
	s.mux.HandleFunc("GET /api/app", s.appInfo)
	s.mux.HandleFunc("GET /api/logs", s.logs)
	s.mux.HandleFunc("POST /api/logs/open", s.openLog)
	s.mux.HandleFunc("PUT /api/debug", s.setDebug)
	s.routeProfiles()
	s.mux.HandleFunc("GET /api/macros", s.macros)
	s.mux.HandleFunc("POST /api/macros", s.saveMacro)
	s.mux.HandleFunc("PUT /api/macros/{id}", s.saveMacro)
	s.mux.HandleFunc("DELETE /api/macros/{id}", s.deleteMacro)
	s.mux.HandleFunc("POST /api/app/restart", s.restart)
	assets, _ := fs.Sub(static, "static")
	s.mux.Handle("GET /", http.FileServerFS(assets))
	return s
}

// polled are the GETs the page repeats every fraction of a second; logging
// them would bury everything else.
var polled = map[string]bool{"/api/record/events": true, "/api/pad": true, "/api/permissions": true, "/api/logs": true, "/api/profiles": true}

// ServeHTTP logs every API call (method, path, status) so a page that misbehaves
// can be followed from deck.log.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/") || (r.Method == http.MethodGet && polled[r.URL.Path] && !debuglog.Enabled()) {
		s.mux.ServeHTTP(w, r)
		return
	}
	rec := &statusRecorder{ResponseWriter: w, status: 200}
	s.mux.ServeHTTP(rec, r)
	log.Printf("%s %s → %d", r.Method, r.URL.Path, rec.status)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func stamp(t time.Time, ok bool) *string {
	if !ok || t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

type keysPayload struct {
	Icons           []deck.IconInfo `json:"icons"`
	Keys            []deck.View     `json:"keys"`
	Modes           []deck.ModeInfo `json:"modes"`
	HotkeyModifiers string          `json:"hotkey_modifiers"`
	SyncedAt        *string         `json:"synced_at"`
	ChangedAt       *string         `json:"changed_at"`
}

func (s *Server) payload(pid int64) (keysPayload, error) {
	views, err := s.store.ViewsIn(pid, s.modifiers)
	if err != nil {
		return keysPayload{}, err
	}
	synced, ok, err := s.store.SyncedAt()
	if err != nil {
		return keysPayload{}, err
	}
	changed, err := s.store.ChangedAt()
	if err != nil {
		return keysPayload{}, err
	}
	return keysPayload{
		Keys: views, Modes: deck.Modes(), Icons: deck.Icons(), HotkeyModifiers: s.modifiers,
		SyncedAt: stamp(synced, ok), ChangedAt: stamp(changed, !changed.IsZero()),
	}, nil
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	p, err := s.payload(profileParam(r))
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) slot(w http.ResponseWriter, r *http.Request) (deck.Slot, bool) {
	slot, err := deck.ParseSlot(r.PathValue("slot"))
	if err != nil {
		writeError(w, 404, err)
		return "", false
	}
	return slot, true
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.slot(w, r)
	if !ok {
		return
	}
	var in struct {
		Label           string      `json:"label"`
		Icon            *string     `json:"icon"`
		Mode            string      `json:"mode"`
		Action          string      `json:"action"`
		Steps           []deck.Step `json:"steps"`
		MacroID         int64       `json:"macro_id"`
		AppScope        string      `json:"app_scope"`
		CooldownSeconds int         `json:"cooldown_seconds"`
		SingleInstance  *bool       `json:"single_instance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	mode, err := deck.ParseMode(in.Mode)
	if err != nil {
		writeError(w, 422, errors.New("mode: "+err.Error()))
		return
	}
	// Without "icon" the key keeps its icon, like the print on its keycap.
	icon := ""
	if in.Icon != nil {
		icon = strings.TrimSpace(*in.Icon)
	} else if old, err := s.store.KeyIn(profileParam(r), slot); err == nil {
		icon = old.Icon
	}
	key := deck.Key{
		ProfileID: profileParam(r), Slot: slot, Label: strings.TrimSpace(in.Label), Icon: icon, Mode: mode, Action: strings.TrimSpace(in.Action),
		Steps: in.Steps, MacroID: in.MacroID, AppScope: strings.TrimSpace(in.AppScope), CooldownSeconds: in.CooldownSeconds,
		SingleInstance: in.SingleInstance == nil || *in.SingleInstance,
	}
	if err := s.store.SaveKey(key); err != nil {
		writeError(w, 422, err)
		return
	}
	saved, err := s.store.KeyIn(profileParam(r), slot)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	last, _ := s.store.LastRun(slot)
	// Saving also writes the pad, so a change takes effect without a separate
	// Apply. The key stays saved when the pad cannot be written (unplugged).
	out := struct {
		deck.View
		Synced    bool   `json:"synced"`
		SyncError string `json:"sync_error,omitempty"`
	}{View: saved.View(s.modifiers, last)}
	if _, err := s.sync(false); err != nil {
		out.SyncError = err.Error()
	} else {
		out.Synced = true
	}
	writeJSON(w, 200, out)
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.slot(w, r)
	if !ok {
		return
	}
	runs, err := s.store.Runs(slot, 20)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	views := make([]deck.RunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, run.View())
	}
	writeJSON(w, 200, map[string]any{"runs": views})
}

func (s *Server) doSync(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") == "1"
	plan, err := s.sync(dryRun)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error(), "plan": plan})
		return
	}
	synced, ok, _ := s.store.SyncedAt()
	writeJSON(w, 200, map[string]any{"plan": plan, "synced_at": stamp(synced, ok)})
}

func (s *Server) press(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.slot(w, r)
	if !ok {
		return
	}
	if s.Press == nil {
		writeError(w, 501, errors.New("this server does not run keys"))
		return
	}
	out, err := s.Press(slot)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ran": out.Ran, "reason": out.Reason, "exit_code": out.ExitCode, "output": out.Output, "summary": out.String()})
}

func (s *Server) mcpInfo(w http.ResponseWriter, r *http.Request) {
	if s.MCP == nil {
		writeError(w, 404, errors.New("no MCP server here"))
		return
	}
	writeJSON(w, 200, s.MCP)
}

func (s *Server) mcpInstall(w http.ResponseWriter, r *http.Request) {
	if s.MCPInstall == nil {
		writeError(w, 501, errors.New("this server cannot install the MCP server"))
		return
	}
	var in struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	message, err := s.MCPInstall(in.Client)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]string{"message": message})
}

func (s *Server) recordStart(w http.ResponseWriter, r *http.Request) {
	if s.Recorder == nil {
		writeError(w, 501, errors.New("recording is not available here"))
		return
	}
	if err := s.Recorder.Start(r.URL.Query().Get("mouse") == "1"); err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"recording": true})
}

func (s *Server) recordStop(w http.ResponseWriter, r *http.Request) {
	if s.Recorder != nil {
		s.Recorder.Stop()
	}
	writeJSON(w, 200, map[string]bool{"recording": false})
}

func (s *Server) recordEvents(w http.ResponseWriter, r *http.Request) {
	if s.Recorder == nil {
		writeJSON(w, 200, map[string]any{"recording": false, "events": []RecordedEvent{}})
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	on, events := s.Recorder.Events(since)
	if events == nil {
		events = []RecordedEvent{}
	}
	writeJSON(w, 200, map[string]any{"recording": on, "events": events})
}

// padPress is a press with the slots whose pad action sends that keystroke.
type padPress struct {
	PadPress
	Slots []deck.Slot `json:"slots"`
}

func (s *Server) padStatus(w http.ResponseWriter, r *http.Request) {
	out := struct {
		PadStatus
		Presses []padPress `json:"presses"`
		Next    int        `json:"next"`
	}{Presses: []padPress{}}
	if s.Pad == nil {
		writeJSON(w, 200, out)
		return
	}
	out.PadStatus = s.Pad.Status()
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	presses, next := s.Pad.Presses(since)
	out.Next = next
	if len(presses) > 0 {
		// What the pad holds now (hotkeys only when profiles switch).
		plan, err := deck.PadPlan(s.store, s.modifiers)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		for _, p := range presses {
			slots := []deck.Slot{}
			for _, op := range plan {
				if pad.Matches(op.Action, p.Combo) {
					slots = append(slots, op.Slot)
				}
			}
			out.Presses = append(out.Presses, padPress{PadPress: p, Slots: slots})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) permissions(w http.ResponseWriter, r *http.Request) {
	list := []Permission{}
	if s.Permissions != nil {
		list = append(list, s.Permissions.List()...)
	}
	writeJSON(w, 200, map[string]any{"permissions": list})
}

func (s *Server) requestPermission(w http.ResponseWriter, r *http.Request) {
	if s.Permissions == nil {
		writeError(w, 501, errors.New("no permissions to ask for here"))
		return
	}
	if err := s.Permissions.Request(r.PathValue("name")); err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"requested": true})
}

func (s *Server) appInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"can_restart": s.Restart != nil})
}

func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	if s.Restart == nil {
		writeError(w, 501, errors.New("restart only works in the desktop app"))
		return
	}
	if err := s.Restart(); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"restarting": true})
}

// logs returns the last lines of the log file (default 200, at most 2000).
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 {
		n = 200
	}
	n = min(n, 2000)
	lines, err := tail(s.LogPath, n)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"path": s.LogPath, "lines": lines, "debug": debuglog.Enabled()})
}

// tail reads the last n lines of a file, looking at most at its last 512 KB.
func tail(path string, n int) ([]string, error) {
	lines := []string{}
	if path == "" {
		return lines, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return lines, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const window = 512 << 10
	start := max(info.Size()-window, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	all := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if start > 0 && len(all) > 0 {
		all = all[1:] // the first line is cut
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return append(lines, all...), nil
}

func (s *Server) openLog(w http.ResponseWriter, r *http.Request) {
	if s.OpenPath == nil || s.LogPath == "" {
		writeError(w, 501, errors.New("no log file to open here"))
		return
	}
	if err := s.OpenPath(s.LogPath); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"opened": true})
}

func (s *Server) setDebug(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := s.store.SetDebug(in.On); err != nil {
		writeError(w, 500, err)
		return
	}
	debuglog.Set(in.On)
	writeJSON(w, 200, map[string]bool{"debug": in.On})
}

// ---- macro library ----------------------------------------------------------

func (s *Server) macros(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.MacrosIn(profileParam(r))
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"macros": list})
}

// saveMacro creates (POST) or replaces (PUT /api/macros/{id}) a macro.
func (s *Server) saveMacro(w http.ResponseWriter, r *http.Request) {
	var m deck.Macro
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeError(w, 400, err)
		return
	}
	status := 201
	m.ID = 0
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, 404, errors.New("no such macro"))
			return
		}
		m.ID, status = id, 200
	}
	id, err := s.store.SaveMacro(m)
	if errors.Is(err, deck.ErrNoMacro) {
		writeError(w, 404, err)
		return
	}
	if err != nil {
		writeError(w, 422, err)
		return
	}
	saved, err := s.store.Macro(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, status, saved)
}

func (s *Server) deleteMacro(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	err := s.store.DeleteMacro(id)
	if errors.Is(err, deck.ErrNoMacro) {
		writeError(w, 404, err)
		return
	}
	if err != nil {
		writeError(w, 500, err)
		return
	}
	// The keys that used it are now off: write the pad so it agrees.
	out := map[string]any{"deleted": true}
	if _, err := s.sync(false); err != nil {
		out["sync_error"] = err.Error()
	}
	writeJSON(w, 200, out)
}
