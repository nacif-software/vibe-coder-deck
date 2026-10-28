// Package mcpserver lets an AI agent configure the pad over the Model Context
// Protocol: the same operations as the page, as tools.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// Deps is what the tools act on.
type Deps struct {
	Store     *deck.Store
	Modifiers string
	Sync      func(dryRun bool) (deck.Plan, error)
	Press     func(slot deck.Slot) (deck.Outcome, error)
	Active    func() int64 // the profile in use (the app in front); nil: the persistent one
	Version   string
}

const instructions = `vibe-coder-deck configures the vibe-coder-keyboard macropad: six keys (key1..key6, two
rows of three) and a knob (knob-left, knob-press, knob-right). The pad can only send keystrokes.
Each slot has a mode:
- off: nothing.
- keys: the pad itself sends a macropad action: a combo such as cmd+shift+4, a sequence of up to
  five comma-separated combos such as "cmd+a, cmd+c" (a comma after "+" is the comma key: opt+,),
  or a media/mouse action such as volume-up, play, click. Modifiers: ctrl, opt, shift, cmd. Use this for system shortcuts (screenshots,
  Spotlight): they ignore synthetic keystrokes. A combo of only modifiers (ropt, ctrl+shift)
  is held while the key is held.
- script: this computer runs a script file (action = path).
- shell: this computer runs a command line (action).
- macro: this computer plays a macro from the library (macro_id; see list_macros / set_macro).
  Kinds: once (each press), repeat (while the key is held), toggle (a press starts, the next
  stops), sequence (steps on press, hold_steps looped while held, release_steps on release).
  standard_delay_ms > 0 replaces recorded waits with a fixed one. Steps: {type: keys, value: combo(s)}, {type: text, value: string},
  {type: delay, value: milliseconds}, {type: launch, value: app name or path}, and recorder steps {type: keydown|keyup, value: key name
  such as cmd, shift, a, enter, f8, a media key (play, next, prev, mute, volume-up, volume-down,
  brightness-up, brightness-down) or code:N for a key code} and {type: click, value: left|right|middle} (at the pointer).
  Synthetic keystrokes; not for system shortcuts.
Profiles: the Desktop and each added app have profiles, each with its own nine controls; with
profile switching on, the app in front picks the profile (list_profiles, profile_id). script/shell/
macro slots can get a cooldown (cooldown_seconds) and ignore presses while a run is in flight (single_instance).
Workflow: set_key for each slot, then sync_pad once to write the pad (nothing reaches the pad
until then). press_key runs a slot now, list_runs shows what happened. show_pad shows the user
the pad as an interactive view (MCP Apps) where they can change keys themselves.`

// New builds the MCP server with every tool registered.
func New(d Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "vibe-coder-deck", Version: d.Version}, &mcp.ServerOptions{Instructions: instructions})
	mcp.AddTool(s, &mcp.Tool{Name: "list_keys", Description: "Every slot of the pad with its mode, action, hotkey, last run, plus the available modes and whether the pad is behind the configuration."}, d.listKeys)
	mcp.AddTool(s, &mcp.Tool{Name: "get_key", Description: "One slot of the pad."}, d.getKey)
	mcp.AddTool(s, &mcp.Tool{Name: "set_key", Description: "Configure one slot. Only the fields given change. Then call sync_pad to write the pad."}, d.setKey)
	mcp.AddTool(s, &mcp.Tool{Name: "sync_pad", Description: "Write the whole configuration into the pad (keys slots get their action, dispatched slots their hotkey, off slots are cleared). dry_run only plans."}, d.syncPad)
	mcp.AddTool(s, &mcp.Tool{Name: "press_key", Description: "Run a script/shell/macro slot now, as if its key were pressed (same lock, cooldown and app scope)."}, d.pressKey)
	mcp.AddTool(s, &mcp.Tool{Name: "list_runs", Description: "Recent runs of a slot, newest first."}, d.listRuns)
	mcp.AddTool(s, &mcp.Tool{Name: "list_profiles", Description: "Apps and their profiles (Desktop first), whether profile switching is on and the persistent profile. Each profile has its own nine controls; pass profile_id to list_keys/get_key/set_key."}, d.listProfiles)
	mcp.AddTool(s, &mcp.Tool{Name: "list_macros", Description: "The macro library: each macro's id, name, kind, steps and the keys it is assigned to."}, d.listMacros)
	mcp.AddTool(s, &mcp.Tool{Name: "set_macro", Description: "Create a macro (no id) or replace one (id). Assign it with set_key mode=macro macro_id=<id>."}, d.setMacro)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_macro", Description: "Delete a macro; keys that used it are turned off and the pad is written."}, d.deleteMacro)
	addApps(s, d)
	return s
}

// Handler serves the MCP server over Streamable HTTP, for `/mcp` on the page's address.
func Handler(d Deps) http.Handler {
	server := New(d)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}

type empty struct{}

type keysOut struct {
	ProfileID       int64           `json:"profile_id" jsonschema:"the profile these keys belong to (list_profiles)"`
	Keys            []deck.View     `json:"keys"`
	Modes           []deck.ModeInfo `json:"modes"`
	HotkeyModifiers string          `json:"hotkey_modifiers" jsonschema:"modifiers of the hotkey dispatched slots send; the digit is the slot position"`
	SyncedAt        *string         `json:"synced_at"`
	ChangedAt       *string         `json:"changed_at"`
	PadBehind       bool            `json:"pad_behind" jsonschema:"true when the pad was not written since the last change: call sync_pad"`
}

func stamp(t time.Time, ok bool) *string {
	if !ok || t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func (d Deps) listKeys(ctx context.Context, req *mcp.CallToolRequest, in profileIn) (*mcp.CallToolResult, keysOut, error) {
	pid := in.ProfileID
	if pid == 0 {
		var err error
		if pid, err = d.Store.PersistentProfile(); err != nil {
			return nil, keysOut{}, err
		}
	}
	views, err := d.Store.ViewsIn(pid, d.Modifiers)
	if err != nil {
		return nil, keysOut{}, err
	}
	synced, ok, err := d.Store.SyncedAt()
	if err != nil {
		return nil, keysOut{}, err
	}
	changed, err := d.Store.ChangedAt()
	if err != nil {
		return nil, keysOut{}, err
	}
	return nil, keysOut{
		ProfileID: pid, Keys: views, Modes: deck.Modes(), HotkeyModifiers: d.Modifiers,
		SyncedAt: stamp(synced, ok), ChangedAt: stamp(changed, !changed.IsZero()),
		PadBehind: !ok || changed.After(synced),
	}, nil
}

type slotIn struct {
	Slot      string `json:"slot" jsonschema:"key1, key2, key3, key4, key5, key6, knob-left, knob-press or knob-right"`
	ProfileID int64  `json:"profile_id,omitempty" jsonschema:"profile (list_profiles); omit for the persistent profile"`
}

type profileIn struct {
	ProfileID int64 `json:"profile_id,omitempty" jsonschema:"profile (list_profiles); omit for the persistent profile"`
}

type keyOut struct {
	Key  deck.View `json:"key"`
	Note string    `json:"note,omitempty"`
}

func (d Deps) view(pid int64, slot deck.Slot) (deck.View, error) {
	k, err := d.Store.KeyIn(pid, slot)
	if err != nil {
		return deck.View{}, err
	}
	last, err := d.Store.LastRun(slot)
	if err != nil {
		return deck.View{}, err
	}
	return k.View(d.Modifiers, last), nil
}

func (d Deps) getKey(ctx context.Context, req *mcp.CallToolRequest, in slotIn) (*mcp.CallToolResult, keyOut, error) {
	slot, err := deck.ParseSlot(in.Slot)
	if err != nil {
		return nil, keyOut{}, err
	}
	v, err := d.view(in.ProfileID, slot)
	return nil, keyOut{Key: v}, err
}

type stepIn struct {
	Type  string `json:"type" jsonschema:"keys, text or delay"`
	Value any    `json:"value" jsonschema:"keys: combo(s) in pad syntax; text: the text to type; delay: milliseconds"`
}

type setIn struct {
	Slot            string   `json:"slot" jsonschema:"key1..key6, knob-left, knob-press or knob-right"`
	ProfileID       int64    `json:"profile_id,omitempty" jsonschema:"profile to change (list_profiles); omit for the persistent profile"`
	Label           *string  `json:"label,omitempty" jsonschema:"short name shown on the page, up to 60 characters"`
	Icon            *string  `json:"icon,omitempty" jsonschema:"drawn on the key's cap: icon:<name> for a built-in drawing (skull, play, pause, next, prev, volume, mute, mic, camera, screenshot, terminal, code, git, bug, rocket, bot, sparkles, bolt, fire, star, heart, check, x, coffee, music, chat, mail, globe, search, lock, settings, folder, home, undo, copy, send, moon), an emoji or up to 4 characters, or empty for none"`
	Mode            *string  `json:"mode,omitempty" jsonschema:"off, keys, script, macro or shell"`
	Action          *string  `json:"action,omitempty" jsonschema:"keys: macropad action (cmd+shift+4, 'cmd+a, cmd+c', volume-up); script: file path; shell: command line"`
	Steps           []stepIn `json:"steps,omitempty" jsonschema:"macro steps, in order; replaces the previous steps"`
	AppScope        *string  `json:"app_scope,omitempty" jsonschema:"only run while this app is in front, e.g. Orca; empty for any app"`
	MacroID         *int64   `json:"macro_id,omitempty" jsonschema:"macro mode: id of the library macro to play (list_macros, set_macro)"`
	CooldownSeconds *int     `json:"cooldown_seconds,omitempty" jsonschema:"ignore further presses for this many seconds after one runs"`
	SingleInstance  *bool    `json:"single_instance,omitempty" jsonschema:"ignore presses while a run of this slot is still going"`
}

func (d Deps) setKey(ctx context.Context, req *mcp.CallToolRequest, in setIn) (*mcp.CallToolResult, keyOut, error) {
	slot, err := deck.ParseSlot(in.Slot)
	if err != nil {
		return nil, keyOut{}, err
	}
	key, err := d.Store.KeyIn(in.ProfileID, slot)
	if err != nil {
		return nil, keyOut{}, err
	}
	if in.Mode != nil {
		if key.Mode, err = deck.ParseMode(*in.Mode); err != nil {
			return nil, keyOut{}, err
		}
	}
	if in.Label != nil {
		key.Label = *in.Label
	}
	if in.Icon != nil {
		key.Icon = *in.Icon
	}
	if in.Action != nil {
		key.Action = *in.Action
	}
	if in.Steps != nil {
		key.Steps = make([]deck.Step, 0, len(in.Steps))
		for _, s := range in.Steps {
			key.Steps = append(key.Steps, deck.Step{Type: s.Type, Value: stepValue(s.Value)})
		}
	}
	if in.MacroID != nil {
		key.MacroID = *in.MacroID
		key.Steps = nil
	}
	if in.Steps != nil && in.MacroID == nil {
		key.MacroID = 0 // steps given: the key gets a macro of its own
	}
	if in.AppScope != nil {
		key.AppScope = *in.AppScope
	}
	if in.CooldownSeconds != nil {
		key.CooldownSeconds = *in.CooldownSeconds
	}
	if in.SingleInstance != nil {
		key.SingleInstance = *in.SingleInstance
	}
	if err := d.Store.SaveKey(key); err != nil {
		return nil, keyOut{}, err
	}
	v, err := d.view(in.ProfileID, slot)
	if err != nil {
		return nil, keyOut{}, err
	}
	return nil, keyOut{Key: v, Note: "saved; call sync_pad to write the pad"}, nil
}

func stepValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

type syncIn struct {
	DryRun bool `json:"dry_run,omitempty" jsonschema:"only report what would be written"`
}

type syncOut struct {
	Plan     []deck.Op `json:"plan"`
	Written  bool      `json:"written"`
	SyncedAt *string   `json:"synced_at"`
}

func (d Deps) syncPad(ctx context.Context, req *mcp.CallToolRequest, in syncIn) (*mcp.CallToolResult, syncOut, error) {
	plan, err := d.Sync(in.DryRun)
	if plan == nil {
		plan = deck.Plan{}
	}
	if err != nil {
		return nil, syncOut{Plan: plan}, fmt.Errorf("pad not written: %w", err)
	}
	synced, ok, _ := d.Store.SyncedAt()
	return nil, syncOut{Plan: plan, Written: !in.DryRun, SyncedAt: stamp(synced, ok)}, nil
}

type pressOut struct {
	Ran      bool   `json:"ran"`
	Reason   string `json:"reason,omitempty" jsonschema:"why it did not run: off, not-dispatched, running, cooldown or app"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Summary  string `json:"summary"`
}

func (d Deps) pressKey(ctx context.Context, req *mcp.CallToolRequest, in slotIn) (*mcp.CallToolResult, pressOut, error) {
	slot, err := deck.ParseSlot(in.Slot)
	if err != nil {
		return nil, pressOut{}, err
	}
	if d.Press == nil {
		return nil, pressOut{}, errors.New("this server cannot run keys")
	}
	out, err := d.Press(slot)
	if err != nil {
		return nil, pressOut{}, err
	}
	return nil, pressOut{Ran: out.Ran, Reason: out.Reason, ExitCode: out.ExitCode, Output: out.Output, Summary: out.String()}, nil
}

type runsIn struct {
	Slot  string `json:"slot" jsonschema:"key1..key6, knob-left, knob-press or knob-right"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many, newest first (default 20)"`
}

type runsOut struct {
	Runs []deck.RunView `json:"runs"`
}

func (d Deps) listRuns(ctx context.Context, req *mcp.CallToolRequest, in runsIn) (*mcp.CallToolResult, runsOut, error) {
	slot, err := deck.ParseSlot(in.Slot)
	if err != nil {
		return nil, runsOut{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	runs, err := d.Store.Runs(slot, limit)
	if err != nil {
		return nil, runsOut{}, err
	}
	views := make([]deck.RunView, 0, len(runs))
	for _, r := range runs {
		views = append(views, r.View())
	}
	return nil, runsOut{Runs: views}, nil
}

type macrosOut struct {
	Macros []deck.Macro `json:"macros"`
}

func (d Deps) listMacros(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, macrosOut, error) {
	list, err := d.Store.Macros()
	return nil, macrosOut{Macros: list}, err
}

type macroIn struct {
	ID              int64    `json:"id,omitempty" jsonschema:"omit to create a macro; give it to replace one"`
	Name            string   `json:"name" jsonschema:"shown in the library, up to 60 characters"`
	Kind            string   `json:"kind,omitempty" jsonschema:"once (default), repeat, toggle or sequence"`
	Steps           []stepIn `json:"steps,omitempty" jsonschema:"what plays; for a sequence, on press"`
	HoldSteps       []stepIn `json:"hold_steps,omitempty" jsonschema:"sequence only: looped while the key is held"`
	ReleaseSteps    []stepIn `json:"release_steps,omitempty" jsonschema:"sequence only: played on release"`
	StandardDelayMs int      `json:"standard_delay_ms,omitempty" jsonschema:"> 0: this wait between actions instead of the recorded ones"`
	Color           string   `json:"color,omitempty" jsonschema:"#rrggbb, shown in the library"`
}

type macroOut struct {
	Macro deck.Macro `json:"macro"`
}

func toSteps(in []stepIn) []deck.Step {
	out := make([]deck.Step, 0, len(in))
	for _, s := range in {
		out = append(out, deck.Step{Type: s.Type, Value: stepValue(s.Value)})
	}
	return out
}

func (d Deps) setMacro(ctx context.Context, req *mcp.CallToolRequest, in macroIn) (*mcp.CallToolResult, macroOut, error) {
	kind := deck.MacroKind(in.Kind)
	if kind == "" {
		kind = deck.MacroOnce
	}
	id, err := d.Store.SaveMacro(deck.Macro{ID: in.ID, Name: in.Name, Kind: kind, Steps: toSteps(in.Steps),
		HoldSteps: toSteps(in.HoldSteps), ReleaseSteps: toSteps(in.ReleaseSteps), StandardDelayMs: in.StandardDelayMs, Color: in.Color})
	if err != nil {
		return nil, macroOut{}, err
	}
	m, err := d.Store.Macro(id)
	return nil, macroOut{Macro: m}, err
}

type macroIDIn struct {
	ID int64 `json:"id" jsonschema:"the macro's id"`
}

func (d Deps) deleteMacro(ctx context.Context, req *mcp.CallToolRequest, in macroIDIn) (*mcp.CallToolResult, syncOut, error) {
	if err := d.Store.DeleteMacro(in.ID); err != nil {
		return nil, syncOut{}, err
	}
	return d.syncPad(ctx, req, syncIn{})
}

type profilesOut struct {
	Apps                []deck.App `json:"apps"`
	Switching           bool       `json:"switching" jsonschema:"true: the app in front picks the profile; false: the persistent profile is used everywhere"`
	PersistentProfileID int64      `json:"persistent_profile_id"`
	ActiveProfileID     int64      `json:"active_profile_id" jsonschema:"the profile in use right now (the app in front, with switching on)"`
}

func (d Deps) listProfiles(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, profilesOut, error) {
	apps, err := d.Store.Apps()
	if err != nil {
		return nil, profilesOut{}, err
	}
	on, _ := d.Store.ProfileSwitching()
	pid, _ := d.Store.PersistentProfile()
	active := pid
	if d.Active != nil {
		if a := d.Active(); a != 0 {
			active = a
		}
	}
	return nil, profilesOut{Apps: apps, Switching: on, PersistentProfileID: pid, ActiveProfileID: active}, nil
}

// profileLabel names a profile the way the page does: "App: Profile".
func (d Deps) profileLabel(pid int64) string {
	p, err := d.Store.Profile(pid)
	if err != nil {
		return fmt.Sprint(pid)
	}
	apps, _ := d.Store.Apps()
	for _, a := range apps {
		if a.ID == p.AppID {
			return a.Name + ": " + p.Name
		}
	}
	return p.Name
}
