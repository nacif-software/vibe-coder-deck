// Package app is the desktop face of vibe-coder-deck: the page in a native
// window (WKWebView on macOS, WebView2 on Windows) plus a menu bar / tray
// icon. Closing the window hides it; the program keeps running until Quit.
package app

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed tray-template.png
var trayTemplate []byte

//go:embed tray-color.png
var trayColor []byte

// Deck is what the window and the menu drive.
type Deck interface {
	Handler() http.Handler
	URL() string
	Sync() (string, error)
	HotkeyError() error
	SetRestart(func())
	Close()
}

// Options wires the desktop app to the rest of the program.
type Options struct {
	Start            func() (Deck, error)
	Notify           func(title, message string)
	LoginItemEnabled func() bool
	SetLoginItem     func(enabled bool) error
	ShowOnStart      bool
	DockVisible      func(visible bool) // in the Dock while the window is open
	Relaunch         func() error       // starts the app again once this one quits
	Version          string
	Icon             []byte
}

// Run starts the deck, shows the window and blocks until Quit is chosen.
func Run(o Options) error {
	deck, startErr := o.Start()
	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "vibe-coder-deck could not start: "+startErr.Error(), http.StatusInternalServerError)
	}))
	if startErr == nil {
		handler = deck.Handler()
		defer deck.Close()
	}

	var window *application.WebviewWindow
	show := func() {
		if window != nil {
			o.DockVisible(true)
			window.Show()
			window.Focus()
		}
	}
	policy := application.ActivationPolicyAccessory
	if o.ShowOnStart {
		policy = application.ActivationPolicyRegular
	}

	app := application.New(application.Options{
		Name:        "Vibe Coder Deck",
		Description: "Configure the vibe-coder-keyboard macropad and run what its keys do",
		Icon:        o.Icon,
		Assets:      application.AssetOptions{Handler: handler, DisableLogging: true},
		Mac: application.MacOptions{
			ActivationPolicy: policy,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "dev.lucasabreu.vibe-coder-deck",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { show() },
		},
	})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Vibe Coder Deck",
		Name:      "main",
		Width:     1360,
		Height:    880,
		MinWidth:  1000,
		MinHeight: 640,
		URL:       "/",
		Hidden:    !o.ShowOnStart,
	})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		o.DockVisible(false)
		e.Cancel()
	})
	// The menu bar while the app is in the Dock: About, Hide, Quit (cmd+Q), Edit, Window.
	app.Menu.SetApplicationMenu(application.DefaultApplicationMenu())

	restart := func() {
		if err := o.Relaunch(); err != nil {
			o.Notify("Restart failed", err.Error())
			return
		}
		app.Quit()
	}
	if startErr == nil {
		deck.SetRestart(restart)
	}
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { show() })

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(trayTemplate)
	tray.SetIcon(trayColor)
	tray.SetTooltip("Vibe Coder Deck " + o.Version)

	menu := app.Menu.New()
	menu.Add("Open Vibe Coder Deck").OnClick(func(*application.Context) { show() })
	if startErr != nil {
		menu.Add("Not running: " + startErr.Error()).SetEnabled(false)
	} else {
		menu.Add("Apply to pad").OnClick(func(*application.Context) {
			summary, err := deck.Sync()
			if err != nil {
				o.Notify("Pad not written", err.Error())
				return
			}
			o.Notify("Pad written", summary)
		})
		const on = "Hotkeys on (ctrl+opt+cmd+1…9)"
		if herr := deck.HotkeyError(); herr != nil {
			log.Printf("hotkeys: %v", herr)
			status := menu.Add("Hotkeys off: allow Accessibility").SetEnabled(false)
			go func() { // the deck registers them by itself once Accessibility is granted
				for deck.HotkeyError() != nil {
					time.Sleep(2 * time.Second)
				}
				status.SetLabel(on)
				menu.Update()
			}()
		} else {
			menu.Add(on).SetEnabled(false)
		}
	}
	menu.AddSeparator()
	login := menu.AddCheckbox("Start at login", o.LoginItemEnabled())
	login.OnClick(func(ctx *application.Context) {
		enable := ctx.ClickedMenuItem().Checked()
		if err := o.SetLoginItem(enable); err != nil {
			o.Notify("Start at login", err.Error())
			ctx.ClickedMenuItem().SetChecked(!enable)
		}
	})
	menu.AddSeparator()
	menu.Add("Restart").OnClick(func(*application.Context) { restart() })
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)

	if startErr != nil {
		o.Notify("Not running", startErr.Error())
	} else if herr := deck.HotkeyError(); herr != nil {
		o.Notify("Hotkeys off", "Allow Vibe Coder Deck in System Settings › Privacy & Security › Accessibility")
	}

	if err := app.Run(); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	fmt.Fprintln(os.Stderr, "vibe-coder-deck: quit")
	return nil
}
