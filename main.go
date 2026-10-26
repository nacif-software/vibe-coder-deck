// vibe-coder-deck configures the vibe-coder-keyboard macropad like a Stream
// Deck and runs what its keys do. One binary: `serve` runs the page, the
// hotkey listener and the dispatcher; the other commands manage the
// configuration from a shell.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/nacif-software/vibe-coder-deck/internal/update"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nacif-software/vibe-coder-deck/internal/app"
	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
	"github.com/nacif-software/vibe-coder-deck/internal/deck"
	"github.com/nacif-software/vibe-coder-deck/internal/mcpserver"
	"github.com/nacif-software/vibe-coder-deck/internal/pad"
	"github.com/nacif-software/vibe-coder-deck/internal/platform"
	"github.com/nacif-software/vibe-coder-deck/internal/web"
)

var version = "dev"

// updateRepo is the GitHub repository whose releases the app installs
// (-ldflags "-X main.updateRepo=owner/name" points elsewhere).
var updateRepo = "nacif-software/vibe-coder-deck"

//go:embed CHANGELOG.md
var changelog string

//go:embed assets/icon-1024.png
var appIcon []byte

const usage = `vibe-coder-deck — configure the vibe-coder-keyboard macropad and run what its keys do

usage: vibe-coder-deck [command] [flags]

  (no command) / app [--addr 127.0.0.1:7071] [--db PATH]
                         the app: native window with the page, menu bar icon, hotkeys
  serve [--addr 127.0.0.1:7071] [--db PATH] [--open]
                         the same without the icon, in the terminal
  sync [--dry-run]       write the configuration into the pad (over USB)
  keys [--json]          list the nine slots
  set <slot> [--mode M] [--label L] [--action A] [--steps JSON] [--app APP]
              [--cooldown N] [--single-instance 0|1]
                         configure one slot (key1..key6, knob-left, knob-press, knob-right)
  import [FILE]          load nehacloud's "php artisan macropad:keys --json" (stdin by default)
  press <slot>           run a slot as if its key were pressed (through the server when it runs)
  mcp [--db PATH]        MCP server on stdio, for an AI agent to configure the pad
                         (the app also serves MCP over HTTP at http://127.0.0.1:7071/mcp)
  mcp install [--client all|claude-code|claude-desktop|cursor|windsurf|codex] [--transport stdio|http]
                         register the MCP server with those AI tools
  update [--install] [--token T] [--clear-token]
                         look for a newer release and show its notes; --install downloads,
                         verifies and installs it; --token keeps a token for a private repository
  version

Data: ` + "%s" + `
`

func main() {
	command, args := "app", []string{}
	if len(os.Args) > 1 {
		command, args = os.Args[1], os.Args[2:]
	}
	var err error
	switch command {
	case "app":
		err = appCmd(args)
	case "serve":
		platform.RunMain(func() { err = serve(args) })
	case "sync":
		err = syncCmd(os.Args[2:])
	case "keys":
		err = keysCmd(os.Args[2:])
	case "set":
		err = setCmd(os.Args[2:])
	case "import":
		err = importCmd(os.Args[2:])
	case "press":
		err = pressCmd(os.Args[2:])
	case "mcp":
		err = mcpCmd(os.Args[2:])
	case "update":
		err = updateCmd(args)
	case "version":
		fmt.Println("vibe-coder-deck", version, runtime.GOOS+"/"+runtime.GOARCH)
	case "-h", "--help", "help":
		fmt.Printf(usage, dataDir())
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		fmt.Fprintf(os.Stderr, usage, dataDir())
		os.Exit(64)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vibe-coder-deck:", err)
		os.Exit(1)
	}
}

func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "vibe-coder-deck")
}

func dbFlag(fs *flag.FlagSet) *string {
	return fs.String("db", filepath.Join(dataDir(), "deck.db"), "SQLite database file")
}

func openStore(path string) (*deck.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return deck.Open(path)
}

// padWriter writes the pad over USB. VCK_MACROPAD names the `macropad` CLI
// of vibe-coder-keyboard to use instead (a path, or "macropad" from PATH).
func padWriter() pad.Writer {
	if cli := os.Getenv("VCK_MACROPAD"); cli != "" {
		return pad.NewCLI(cli)
	}
	return pad.NewUSB()
}

// deckRuntime is the running program: store, dispatcher, page and hotkeys.
type deckRuntime struct {
	updater     *update.Updater
	updCancel   context.CancelFunc
	store       *deck.Store
	addr        string
	handler     http.Handler
	httpServer  *http.Server
	httpErr     chan error
	restart     func()     // set by the desktop app: relaunch and quit
	mu          sync.Mutex // guards stopHotkeys, hotkeyErr and restart
	stopHotkeys func()
	hotkeyErr   error
	logFile     *os.File
}

// startDeck opens the store, starts the page and registers the hotkeys. A
// hotkey registration failure is kept in hotkeyErr rather than returned, so
// the app can still show the page and explain what to grant.
func startDeck(addr, dbPath string) (*deckRuntime, error) {
	store, err := openStore(dbPath)
	if err != nil {
		return nil, err
	}
	r := &deckRuntime{store: store, addr: addr, httpErr: make(chan error, 1)}

	if f, err := os.OpenFile(filepath.Join(dataDir(), "deck.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		r.logFile = f
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}

	dispatcher := &deck.Dispatcher{
		Store: store, Gate: deck.NewGate(store), Runner: platform.Runner{}, Frontmost: platform.Frontmost,
		Log: log.Printf,
	}
	press := func(slot deck.Slot) (deck.Outcome, error) {
		out, err := dispatcher.Press(slot)
		if err != nil {
			platform.Notify("Error ("+string(slot)+")", err.Error())
		} else if out.Ran && out.ExitCode != 0 {
			platform.Notify(fmt.Sprintf("Failed (%s, exit %d)", slot, out.ExitCode), lastLines(out.Output, 2))
		}
		return out, err
	}

	server := web.New(store, platform.HotkeyModifiers, func(dryRun bool) (deck.Plan, error) {
		return deck.Sync(store, padWriter(), platform.HotkeyModifiers, dryRun)
	})
	server.Press = press
	server.Recorder = platformRecorder{}
	server.Pad = platformPad{}
	server.Permissions = platformPermissions{}
	server.Apps = platformApps{}
	// The profile in use follows the app in front (when switching is on).
	var active atomic.Int64
	var front atomic.Value // [2]string{bundleID, name}
	resolve := func() {
		bundle, name, path := platform.FrontmostApp()
		front.Store([2]string{bundle, name})
		pid := store.ResolveProfile(bundle, name, path)
		if old := active.Swap(pid); old != pid {
			log.Printf("profile %d (%s in front)", pid, name)
		}
	}
	resolve()
	go func() {
		for range time.Tick(400 * time.Millisecond) {
			resolve()
		}
	}()
	dispatcher.Profile = func() int64 { return active.Load() }
	server.Active = func() (int64, string, string) {
		f, _ := front.Load().([2]string)
		return active.Load(), f[0], f[1]
	}
	server.LogPath = filepath.Join(dataDir(), "deck.log")
	server.OpenPath = func(path string) error { openBrowser(path); return nil }
	if on, err := store.Debug(); err == nil {
		debuglog.Set(on)
	}
	server.Restart = func() error {
		r.mu.Lock()
		restart := r.restart
		r.mu.Unlock()
		if restart == nil {
			return errors.New("restart only works in the desktop app")
		}
		go restart()
		return nil
	}
	up := newUpdater(store)
	update.CleanOld(up.Target)
	server.Updates, server.Version = up, version
	r.updater = up
	uctx, cancelUpdates := context.WithCancel(context.Background())
	r.updCancel = cancelUpdates
	go up.Run(uctx, 20*time.Second, 6*time.Hour)
	platform.WatchPad()
	syncFn := func(dryRun bool) (deck.Plan, error) {
		return deck.Sync(store, padWriter(), platform.HotkeyModifiers, dryRun)
	}
	mcpURL := "http://" + addr + "/mcp"
	server.MCP = &web.MCPInfo{URL: mcpURL, Executable: executablePath(), Clients: clientsAny()}
	server.MCPInstall = func(client string) (string, error) {
		res, err := newInstaller(mcpURL, "stdio").Install(client)
		return res.Message, err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.Handler(mcpserver.Deps{Store: store, Modifiers: platform.HotkeyModifiers, Sync: syncFn, Press: press, Version: version, Active: func() int64 { return active.Load() }}))
	mux.Handle("/", server)
	r.handler = mux
	r.httpServer = &http.Server{Addr: addr, Handler: mux}
	go func() { r.httpErr <- r.httpServer.ListenAndServe() }()

	onKey := func(digit int, down bool) {
		slot := deck.Slots()[digit-1]
		debuglog.Printf("hotkey %s+%d %s → %s", platform.HotkeyModifiers, digit, map[bool]string{true: "down", false: "up"}[down], slot)
		if !down {
			dispatcher.Up(slot)
			return
		}
		if done := dispatcher.Down(slot); done != nil {
			go func() {
				out := <-done
				if out.Ran && out.ExitCode != 0 {
					platform.Notify(fmt.Sprintf("Failed (%s, exit %d)", slot, out.ExitCode), lastLines(out.Output, 2))
				}
			}()
		}
	}
	r.stopHotkeys, r.hotkeyErr = platform.Listen(onKey)
	if r.hotkeyErr != nil {
		log.Printf("hotkeys: %v", r.hotkeyErr)
	}
	log.Printf("vibe-coder-deck %s: page at %s, hotkeys %s+1..9 %s, data in %s", version, r.URL(), platform.HotkeyModifiers, map[bool]string{true: "registered", false: "NOT registered"}[r.hotkeyErr == nil], dataDir())
	if r.hotkeyErr != nil {
		if !platform.AccessibilityAllowed() {
			platform.RequestPermission(platform.PermAccessibility)
		}
		go r.retryHotkeys(onKey)
	}
	return r, nil
}

func (r *deckRuntime) URL() string { return "http://" + r.addr }

// Handler serves the page, the API and MCP; the desktop window loads it directly.
func (r *deckRuntime) Handler() http.Handler { return r.handler }

func (r *deckRuntime) HotkeyError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hotkeyErr
}

// SetRestart lets the desktop app say how it restarts itself.
func (r *deckRuntime) SetRestart(f func()) {
	r.mu.Lock()
	r.restart = f
	r.mu.Unlock()
}

// retryHotkeys registers the hotkeys once Accessibility is granted, so the
// app does not have to be relaunched.
func (r *deckRuntime) retryHotkeys(onKey func(digit int, down bool)) {
	for {
		time.Sleep(2 * time.Second)
		if !platform.AccessibilityAllowed() {
			continue
		}
		stop, err := platform.Listen(onKey)
		r.mu.Lock()
		r.stopHotkeys, r.hotkeyErr = stop, err
		r.mu.Unlock()
		if err == nil {
			log.Printf("hotkeys %s+1..9 registered", platform.HotkeyModifiers)
			return
		}
	}
}

// Sync writes the pad and describes what was written.
func (r *deckRuntime) Sync() (string, error) {
	plan, err := deck.Sync(r.store, padWriter(), platform.HotkeyModifiers, false)
	if err != nil {
		if errors.Is(err, pad.ErrNotConnected) {
			return "", fmt.Errorf("%w: plug the pad in and try again", err)
		}
		return "", err
	}
	written := 0
	for _, op := range plan {
		if op.Action != "" {
			written++
		}
	}
	return fmt.Sprintf("%d slots set, %d cleared", written, len(plan)-written), nil
}

func (r *deckRuntime) Close() {
	if r.updCancel != nil {
		r.updCancel()
	}
	r.mu.Lock()
	if r.stopHotkeys != nil {
		r.stopHotkeys()
	}
	r.mu.Unlock()
	if r.httpServer != nil {
		r.httpServer.Close()
	}
	r.store.Close()
	if r.logFile != nil {
		r.logFile.Close()
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7071", "address of the page")
	db := dbFlag(fs)
	open := fs.Bool("open", false, "open the page in the browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := startDeck(*addr, *db)
	if err != nil {
		return err
	}
	defer r.Close()
	if r.hotkeyErr != nil {
		return r.hotkeyErr
	}
	if *open {
		openBrowser(r.URL())
	}
	return <-r.httpErr
}

func appCmd(args []string) error {
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7071", "address of the page")
	db := dbFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	executable, _ := os.Executable()
	return app.Run(app.Options{
		Start:            func() (app.Deck, error) { return startDeck(*addr, *db) },
		Notify:           platform.Notify,
		LoginItemEnabled: platform.LoginItemEnabled,
		SetLoginItem:     func(enabled bool) error { return platform.SetLoginItem(enabled, executable, dataDir()) },
		ShowOnStart:      os.Getenv("VCK_LOGIN_START") != "1",
		DockVisible:      platform.SetDockVisible,
		Relaunch:         platform.Relaunch,
		Version:          version,
		Icon:             appIcon,
	})
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func syncCmd(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	db := dbFlag(fs)
	dryRun := fs.Bool("dry-run", false, "print what would be written and touch nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	plan, err := deck.Sync(store, padWriter(), platform.HotkeyModifiers, *dryRun)
	for _, op := range plan {
		fmt.Println(" ", op)
	}
	if err != nil {
		if errors.Is(err, pad.ErrNotConnected) {
			return fmt.Errorf("%w: plug the pad in and run sync again", err)
		}
		return err
	}
	if *dryRun {
		fmt.Println("dry run: nothing written")
	} else {
		fmt.Println("written to the pad")
	}
	return nil
}

func keysCmd(args []string) error {
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	db := dbFlag(fs)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	views, err := store.Views(platform.HotkeyModifiers)
	if err != nil {
		return err
	}
	if *asJSON {
		synced, ok, _ := store.SyncedAt()
		changed, _ := store.ChangedAt()
		payload := map[string]any{"ok": true, "hotkey_modifiers": platform.HotkeyModifiers, "keys": views, "synced_at": nil, "changed_at": nil}
		if ok {
			payload["synced_at"] = synced.Format(time.RFC3339)
		}
		if !changed.IsZero() {
			payload["changed_at"] = changed.Format(time.RFC3339)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	fmt.Printf("%-11s %-7s %-16s %-40s %-8s %s\n", "SLOT", "MODE", "PAD SENDS", "RUNS", "APP", "COOLDOWN")
	for _, v := range views {
		padAction := "—"
		if v.PadAction != nil {
			padAction = *v.PadAction
		}
		runs := "—"
		if v.Dispatched {
			runs = v.Action
			if v.Mode == deck.ModeMacro {
				runs = fmt.Sprintf("%d steps", len(v.Steps))
			}
		}
		app := v.AppScope
		if app == "" {
			app = "any"
		}
		fmt.Printf("%-11s %-7s %-16s %-40s %-8s %ds\n", v.Slot, v.Mode, padAction, truncate(runs, 40), app, v.CooldownSeconds)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func setCmd(args []string) error {
	if len(args) < 1 {
		return errors.New("set needs a slot")
	}
	slot, err := deck.ParseSlot(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	db := dbFlag(fs)
	mode := fs.String("mode", "", "off, keys, script, macro or shell")
	label := fs.String("label", "", "short name shown on the page")
	action := fs.String("action", "", "macropad action (keys), script path (script) or command line (shell)")
	steps := fs.String("steps", "", `macro steps as JSON: [{"type":"keys","value":"cmd+a"},{"type":"delay","value":200}]`)
	app := fs.String("app", "", "app that must be in front; empty for any")
	cooldown := fs.Int("cooldown", -1, "seconds to ignore further presses after one runs")
	single := fs.String("single-instance", "", "1 to ignore presses while a run is in flight, 0 to allow overlap")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	key, err := store.Key(slot)
	if err != nil {
		return err
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if given["mode"] {
		if key.Mode, err = deck.ParseMode(*mode); err != nil {
			return err
		}
	}
	if given["label"] {
		key.Label = *label
	}
	if given["action"] {
		key.Action = *action
	}
	if given["steps"] {
		key.Steps = nil
		if err := json.Unmarshal([]byte(*steps), &key.Steps); err != nil {
			return fmt.Errorf("--steps: %w", err)
		}
	}
	if given["app"] {
		key.AppScope = *app
	}
	if given["cooldown"] {
		key.CooldownSeconds = *cooldown
	}
	if given["single-instance"] {
		key.SingleInstance = *single == "1" || strings.EqualFold(*single, "true")
	}
	if err := store.SaveKey(key); err != nil {
		return err
	}
	padAction, ok := key.PadAction(platform.HotkeyModifiers)
	if !ok {
		padAction = "nothing"
	}
	fmt.Printf("%s: %s → pad sends %s\n", slot, key.Mode, padAction)
	return nil
}

func importCmd(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	db := dbFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := io.Reader(os.Stdin)
	if fs.NArg() > 0 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	var payload struct {
		Keys []struct {
			Slot            string      `json:"slot"`
			Label           *string     `json:"label"`
			Mode            string      `json:"mode"`
			Action          *string     `json:"action"`
			Steps           []deck.Step `json:"steps"`
			AppScope        *string     `json:"app_scope"`
			CooldownSeconds int         `json:"cooldown_seconds"`
			SingleInstance  bool        `json:"single_instance"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(in).Decode(&payload); err != nil {
		return fmt.Errorf("reading keys JSON: %w", err)
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	for _, k := range payload.Keys {
		slot, err := deck.ParseSlot(k.Slot)
		if err != nil {
			return err
		}
		mode, err := deck.ParseMode(k.Mode)
		if err != nil {
			return fmt.Errorf("%s: %w", slot, err)
		}
		key := deck.Key{Slot: slot, Label: deref(k.Label), Mode: mode, Action: deref(k.Action), Steps: k.Steps,
			AppScope: deref(k.AppScope), CooldownSeconds: k.CooldownSeconds, SingleInstance: k.SingleInstance}
		if err := store.SaveKey(key); err != nil {
			return fmt.Errorf("%s: %w", slot, err)
		}
		fmt.Printf("%s: %s\n", slot, mode)
	}
	fmt.Printf("imported %d keys; run `vibe-coder-deck sync` to write the pad\n", len(payload.Keys))
	return nil
}

func pressCmd(args []string) error {
	fs := flag.NewFlagSet("press", flag.ContinueOnError)
	db := dbFlag(fs)
	addr := fs.String("addr", "127.0.0.1:7071", "address of a running server to press through")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("press needs a slot")
	}
	slot, err := deck.ParseSlot(fs.Arg(0))
	if err != nil {
		return err
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	out, err := pressThroughAppOrLocal(*addr, store)(slot)
	if err != nil {
		return err
	}
	fmt.Println(out)
	if out.Output != "" {
		fmt.Println(out.Output)
	}
	return nil
}

// pressThroughAppOrLocal presses through the running app when there is one,
// so its lock is the one that counts, and dispatches locally otherwise.
func pressThroughAppOrLocal(addr string, store *deck.Store) func(deck.Slot) (deck.Outcome, error) {
	return func(slot deck.Slot) (deck.Outcome, error) {
		client := &http.Client{Timeout: 20 * time.Minute}
		if res, err := client.Post("http://"+addr+"/api/press/"+string(slot), "application/json", nil); err == nil {
			defer res.Body.Close()
			var out struct {
				Ran      bool   `json:"ran"`
				Reason   string `json:"reason"`
				ExitCode int    `json:"exit_code"`
				Output   string `json:"output"`
				Error    string `json:"error"`
			}
			if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
				return deck.Outcome{Slot: slot}, err
			}
			if out.Error != "" {
				return deck.Outcome{Slot: slot}, errors.New(out.Error)
			}
			return deck.Outcome{Slot: slot, Ran: out.Ran, Reason: out.Reason, ExitCode: out.ExitCode, Output: out.Output}, nil
		}
		d := &deck.Dispatcher{Store: store, Gate: deck.NewGate(store), Runner: platform.Runner{}, Frontmost: platform.Frontmost, Log: log.Printf}
		return d.Press(slot)
	}
}

func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "vibe-coder-deck"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

func clientsAny() []any {
	out := []any{}
	for _, c := range mcpserver.Clients() {
		out = append(out, c)
	}
	return out
}

func newInstaller(url, transport string) mcpserver.Installer {
	home, _ := os.UserHomeDir()
	cfg, _ := os.UserConfigDir()
	return mcpserver.Installer{Executable: executablePath(), URL: url, Home: home, ConfigDir: cfg, Transport: transport}
}

// mcpInstallCmd registers the MCP server with AI tools: `mcp install --client all`.
func mcpInstallCmd(args []string) error {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	client := fs.String("client", "all", "claude-code, claude-desktop, cursor, windsurf, codex or all")
	transport := fs.String("transport", "stdio", "stdio (works without the app) or http (through the running app)")
	addr := fs.String("addr", "127.0.0.1:7071", "address of the app, for http")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inst := newInstaller("http://"+*addr+"/mcp", *transport)
	ids := []string{*client}
	if *client == "all" {
		ids = ids[:0]
		for _, c := range mcpserver.Clients() {
			ids = append(ids, c.ID)
		}
	}
	failed := 0
	for _, id := range ids {
		res, err := inst.Install(id)
		if err != nil {
			failed++
			fmt.Printf("%-15s skipped: %v\n", id, err)
			continue
		}
		fmt.Printf("%-15s %s\n", id, strings.SplitN(res.Message, "\n", 2)[0])
	}
	if failed == len(ids) {
		return errors.New("nothing installed")
	}
	return nil
}

// mcpCmd speaks MCP on stdin/stdout, for `claude mcp add vibe-coder-deck -- vibe-coder-deck mcp`.
func mcpCmd(args []string) error {
	if len(args) > 0 && args[0] == "install" {
		return mcpInstallCmd(args[1:])
	}
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	db := dbFlag(fs)
	addr := fs.String("addr", "127.0.0.1:7071", "address of a running app to press keys through")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	log.SetOutput(os.Stderr) // stdout is the protocol
	server := mcpserver.New(mcpserver.Deps{
		Store: store, Modifiers: platform.HotkeyModifiers, Version: version,
		Sync: func(dryRun bool) (deck.Plan, error) {
			return deck.Sync(store, padWriter(), platform.HotkeyModifiers, dryRun)
		},
		Press:  pressThroughAppOrLocal(*addr, store),
		Active: activeThroughApp(*addr),
	})
	return server.Run(context.Background(), &sdkmcp.StdioTransport{})
}

// activeThroughApp asks the running app which profile is in use (the app in
// front); 0 when the app is not running, so the persistent profile is used.
func activeThroughApp(addr string) func() int64 {
	return func() int64 {
		client := &http.Client{Timeout: 2 * time.Second}
		res, err := client.Get("http://" + addr + "/api/profiles")
		if err != nil {
			return 0
		}
		defer res.Body.Close()
		var out struct {
			ActiveProfileID int64 `json:"active_profile_id"`
		}
		if json.NewDecoder(res.Body).Decode(&out) != nil {
			return 0
		}
		return out.ActiveProfileID
	}
}

// platformRecorder adapts the OS recorder to the web server.
type platformRecorder struct{}

func (platformRecorder) Start(mouse bool) error { return platform.StartRecording(mouse) }

func (platformRecorder) Stop() { platform.StopRecording() }

func (platformRecorder) Events(since int) (bool, []web.RecordedEvent) {
	on, events := platform.Recording(since)
	out := make([]web.RecordedEvent, 0, len(events))
	for _, e := range events {
		out = append(out, web.RecordedEvent{Type: e.Type, Value: e.Value, At: e.At})
	}
	return on, out
}

// platformPad adapts the OS pad watcher to the web server.
type platformPad struct{}

func (platformPad) Status() web.PadStatus {
	devices, inputAllowed := platform.PadStatus()
	st := web.PadStatus{Connected: len(devices) > 0, InputAllowed: inputAllowed}
	for _, d := range devices {
		st.VendorID, st.ProductID = d.VendorID, d.ProductID
		if st.Product == "" {
			st.Product = d.Product
		}
		if st.Manufacturer == "" {
			st.Manufacturer = d.Manufacturer
		}
	}
	return st
}

func (platformPad) Presses(since int) ([]web.PadPress, int) {
	presses, next := platform.PadPresses(since)
	out := make([]web.PadPress, 0, len(presses))
	for _, p := range presses {
		out = append(out, web.PadPress{Combo: p.Combo, At: p.At})
	}
	return out, next
}

// platformPermissions adapts the OS permission checks to the web server.
type platformPermissions struct{}

func (platformPermissions) List() []web.Permission {
	var out []web.Permission
	for _, p := range platform.Permissions() {
		out = append(out, web.Permission{Name: p.Name, Label: p.Label, Why: p.Why, Granted: p.Granted})
	}
	return out
}

func (platformPermissions) Request(name string) error { return platform.RequestPermission(name) }

// platformApps adapts the OS's app listing to the web server.
type platformApps struct{}

func toAppInfo(a platform.InstalledApp) web.AppInfo {
	return web.AppInfo{Name: a.Name, BundleID: a.BundleID, Path: a.Path}
}

func (platformApps) Installed() []web.AppInfo {
	var out []web.AppInfo
	for _, a := range platform.InstalledApps() {
		out = append(out, toAppInfo(a))
	}
	return out
}

func (platformApps) Info(path string) (web.AppInfo, error) {
	a, err := platform.AppInfo(path)
	return toAppInfo(a), err
}

func (platformApps) Choose() (web.AppInfo, bool, error) {
	a, ok, err := platform.ChooseApp()
	return toAppInfo(a), ok, err
}

func (platformApps) Icon(path string) ([]byte, error) { return platform.AppIconPNG(path, 128) }

// Updater is the app's release updater.
func (r *deckRuntime) Updater() *update.Updater { return r.updater }

// githubToken is the token for a private releases repository: the
// environment first, then the one kept with `update --token`.
func githubToken(store *deck.Store) string {
	if t := os.Getenv("VCD_GITHUB_TOKEN"); t != "" {
		return t
	}
	t, _ := store.GitHubToken()
	return t
}

func newUpdater(store *deck.Store) *update.Updater {
	up := update.New(update.Source{Repo: updateRepo, Token: githubToken(store)}, version, update.TargetFromExecutable(executablePath()))
	up.Changelog = changelog
	if auto, err := store.AutoUpdate(); err == nil {
		up.SetAuto(auto)
	}
	return up
}

// updateCmd looks for a newer release and shows its notes; --install
// downloads, verifies and installs it; --token keeps a GitHub token for a
// private releases repository.
func updateCmd(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	db := dbFlag(fs)
	install := fs.Bool("install", false, "download, verify and install the newest release")
	token := fs.String("token", "", "keep a GitHub token for reading a private releases repository")
	clear := fs.Bool("clear-token", false, "forget the kept token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := deck.Open(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	if *token != "" || *clear {
		tok := *token
		if *clear {
			tok = ""
		}
		if err := store.SetGitHubToken(tok); err != nil {
			return err
		}
		fmt.Println(map[bool]string{true: "token kept", false: "token forgotten"}[tok != ""])
		return nil
	}
	up := newUpdater(store)
	ctx := context.Background()
	st, err := up.Check(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("current: %s\nlatest:  %s (%s)\n", st.Current, st.Latest.Version, st.Latest.PublishedAt.Format("2006-01-02"))
	if st.Latest.Notes != "" {
		fmt.Println("\n" + strings.TrimSpace(st.Latest.Notes) + "\n")
	}
	if !*install {
		if st.State == update.StateAvailable {
			fmt.Println("a newer release: run `vibe-coder-deck update --install` to install it")
		} else {
			fmt.Println("up to date")
		}
		return nil
	}
	if up.Target == "" {
		return errors.New("this binary is not the installed app (Vibe Coder Deck.app or vibe-coder-deck.exe): install the release by hand")
	}
	fmt.Printf("downloading %s…\n", st.Latest.Version)
	if _, err := up.Download(ctx); err != nil {
		return err
	}
	fmt.Println("verified; installing…")
	if err := up.Install(); err != nil {
		return err
	}
	fmt.Printf("installed %s into %s; open the app again to run it\n", st.Latest.Version, up.Target)
	return nil
}
