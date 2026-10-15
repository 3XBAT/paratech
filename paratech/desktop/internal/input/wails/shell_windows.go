//go:build windows

package wails

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Tray menu labels.
const (
	menuOpen      = "Open Learning Assistant"
	menuShowPopup = "Show recording popup"
	menuQuit      = "Quit"
)

// windowsUI is the running Wails application.
type windowsUI struct {
	app    *application.App
	main   *application.WebviewWindow
	popup  *application.WebviewWindow
	toasts pusher
}

func (s *Shell) run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil // stopped before the UI appeared: nothing to show or tear down
	}

	// The single-instance callback runs after New returns; it reads u under the shell lock through ShowMain.
	app := application.New(appOptions(s.opts, func() { _ = s.ShowMain() }))

	// Only the first instance gets here, so a second launch never opens the database (ADR-0007).
	b, err := s.openBindings(ctx)
	if err != nil {
		return err
	}
	app.RegisterService(application.NewService(b.settings))
	app.RegisterService(application.NewService(b.activity))
	toasts := newToastService(notifications.New(), s.opts.Log)
	app.RegisterService(application.NewService(toasts))

	u := &windowsUI{app: app, toasts: toasts}
	var offs []func()
	u.main, offs = newWindow(app, mainWindowSpec(s.opts.Name), offs)
	u.popup, offs = newWindow(app, popupWindowSpec(s.opts.Name), offs)
	// Wails closes every window on shutdown; without the hide-on-close hooks that close would be cancelled.
	app.OnShutdown(func() {
		for _, off := range offs {
			off()
		}
	})

	newTray(app, u, s.opts.Name)

	started := make(chan struct{})
	var once sync.Once
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		once.Do(func() { close(started) })
	})

	// Quit is a no-op until the platform app exists, so a cancellation waits for the start event.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-started:
		case <-done:
			return
		}
		select {
		case <-ctx.Done():
			app.Quit()
		case <-done:
		}
	}()

	s.setUI(u)
	s.opts.Log.Info("ui shell starting")
	return app.Run()
}

func appOptions(opts Options, onSecondInstance func()) application.Options {
	return application.Options{
		Name:   opts.Name,
		Logger: opts.Log,
		// The asset server logs every request at info level; the pages are embedded, so the noise has no value.
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets()), DisableLogging: true},
		// main.go owns signals through its context; the Wails handler would quit behind its back.
		DisableDefaultSignalHandler: true,
		// The app lives in the tray: closing (hiding) the windows must not end it.
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: opts.InstanceID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				onSecondInstance()
			},
		},
	}
}

// newWindow creates a window (it appears when the app runs) and returns it with the hooks to remove on shutdown.
func newWindow(app *application.App, spec windowSpec, offs []func()) (*application.WebviewWindow, []func()) {
	w := app.Window.NewWithOptions(windowOptions(spec))
	if spec.HideOnClose {
		offs = append(offs, w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			w.Hide()
			e.Cancel()
		}))
	}
	return w, offs
}

// windowOptions maps a spec to Wails options.
func windowOptions(spec windowSpec) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Name:          spec.Name,
		Title:         spec.Title,
		URL:           spec.URL,
		Width:         spec.Width,
		Height:        spec.Height,
		Frameless:     spec.Frameless,
		AlwaysOnTop:   spec.AlwaysOnTop,
		Hidden:        spec.Hidden,
		DisableResize: spec.DisableResize,
		// The zero colour is transparent black, which shows through until the page paints.
		BackgroundColour: application.NewRGB(255, 255, 255),
		Windows:          application.WindowsWindow{HiddenOnTaskbar: spec.HiddenOnTaskbar},
	}
	if spec.NoActivate {
		// Wails has no "no activate" option and dropped WS_EX_NOACTIVATE from HiddenOnTaskbar, and a non-zero ExStyle
		// replaces its computed extended style, so the whole set is spelled out here.
		opts.Windows.ExStyle = popupExStyle(spec)
	}
	return opts
}

func popupExStyle(spec windowSpec) int {
	style := w32.WS_EX_CONTROLPARENT | w32.WS_EX_NOACTIVATE
	if spec.AlwaysOnTop {
		style |= w32.WS_EX_TOPMOST
	}
	if spec.HiddenOnTaskbar {
		style |= w32.WS_EX_TOOLWINDOW
	} else {
		style |= w32.WS_EX_APPWINDOW
	}
	return style
}

func newTray(app *application.App, u *windowsUI, name string) {
	menu := application.NewMenu()
	menu.Add(menuOpen).OnClick(func(*application.Context) { _ = u.showMain() })
	menu.Add(menuShowPopup).OnClick(func(*application.Context) { _ = u.showPopup() })
	menu.AddSeparator()
	menu.Add(menuQuit).OnClick(func(*application.Context) { app.Quit() })

	tray := app.SystemTray.New()
	tray.SetTooltip(name)
	tray.SetMenu(menu)
	tray.OnClick(func() { _ = u.showMain() })
}

func (u *windowsUI) showPopup() error {
	if scr := u.app.Screen.GetPrimary(); scr != nil {
		wa := scr.WorkArea
		x, y := popupOrigin(rect{X: wa.X, Y: wa.Y, Width: wa.Width, Height: wa.Height}, popupWidth, popupHeight, popupMargin)
		u.popup.SetPosition(x, y)
	}

	// Wails shows windows with SW_SHOW, which activates even a WS_EX_NOACTIVATE window (checked on Windows 10).
	// Making it visible with SW_SHOWNOACTIVATE first turns that SW_SHOW into a no-op for activation; Wails' Show is
	// still needed because it also makes the WebView visible.
	application.InvokeSync(func() {
		if h := u.popup.NativeWindow(); h != nil {
			w32.ShowWindow(w32.HWND(uintptr(h)), w32.SW_SHOWNOACTIVATE)
		}
	})
	u.popup.Show()
	return nil
}

func (u *windowsUI) hidePopup() error {
	u.popup.Hide()
	return nil
}

func (u *windowsUI) showMain() error {
	u.main.Show()
	u.main.Focus()
	return nil
}

func (u *windowsUI) notify(ctx context.Context, t toast) error { return pushWithin(ctx, u.toasts, t) }
