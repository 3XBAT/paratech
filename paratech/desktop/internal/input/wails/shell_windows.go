//go:build windows

package wails

import (
	"context"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// Tray menu labels.
const (
	menuOpen      = "Open paratech"
	menuShowPopup = "Show recording popup"
	menuQuit      = "Quit"
)

// windowsUI is the running Wails application.
type windowsUI struct {
	app   *application.App
	main  *application.WebviewWindow
	popup *application.WebviewWindow
}

func (s *Shell) run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil // stopped before the UI appeared: nothing to show or tear down
	}

	// A second instance exits inside application.New after notifying the first one, so it must hand its foreground
	// right on before that: Windows lets only the foreground process (here the browser's child that opened a paratech:
	// link) give the first instance's SetForegroundWindow a chance; otherwise the window only flashes in the taskbar.
	allowForegroundHandOff()

	// The single-instance callback runs after New returns; it reads u under the shell lock through ShowMain.
	opts := appOptions(s.opts, func() { _ = s.ShowMain() })
	media := withMedia(&opts, s.opts.Log)
	app := application.New(opts)

	// Only the first instance gets here, so a second launch never opens the database (ADR-0007).
	b, err := s.OpenServices(ctx)
	if err != nil {
		return err
	}
	app.RegisterService(application.NewService(b.Settings))
	app.RegisterService(application.NewService(b.Activity))
	app.RegisterService(application.NewService(b.Prompt))
	app.RegisterService(application.NewService(b.Popup))
	app.RegisterService(application.NewService(b.Schedule))
	app.RegisterService(application.NewService(b.Models))
	app.RegisterService(application.NewService(b.Speech))
	app.RegisterService(application.NewService(b.Connections))
	app.RegisterService(application.NewService(b.Google))
	app.RegisterService(application.NewService(b.Calendar))
	app.RegisterService(application.NewService(b.Diagnostics))
	app.RegisterService(application.NewService(b.Help))
	app.RegisterService(application.NewService(b.Homework))
	app.RegisterService(application.NewService(b.Transcript))
	app.RegisterService(application.NewService(b.Summary))
	media.setSource(b.Transcript.uc) // the media handler plays recordings through the transcript use case

	u := &windowsUI{app: app}
	var offs []func()
	u.main, offs = newWindow(app, mainWindowSpec(s.opts.Name), offs)
	u.popup, offs = newWindow(app, popupWindowSpec(s.opts.Name), offs)
	// Wails closes every window on shutdown; without the hide-on-close hooks that close would be cancelled.
	app.OnShutdown(func() {
		for _, off := range offs {
			off()
		}
	})

	// Menu handlers run on goroutines, so the offer may show the popup synchronously.
	newTray(app, u, s.opts.Name, func() { b.Prompt.offerAdHoc(context.Background()) })

	started := make(chan struct{})
	var once sync.Once
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		once.Do(func() {
			close(started)
			if s.opts.Started != nil {
				s.opts.Started()
			}
		})
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
		Name: opts.Name,
		// The window icon: the exe has no icon resource yet (ADR-0015), so Wails takes this one.
		Icon: appIcon(),
		// Wails logs binding call arguments (the mail password among them) at Debug, so it never gets Debug.
		Logger: wailsLogger(opts.Log),
		// The asset server logs every request at info level; the pages are embedded, so the noise has no value.
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets()), DisableLogging: true},
		// main.go owns signals through its context; the Wails handler would quit behind its back.
		DisableDefaultSignalHandler: true,
		// The app lives in the tray: closing (hiding) the windows must not end it.
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			// A fixed folder, so the WebView2 state (localStorage: the UI theme) survives the exe rename (ADR-0015).
			WebviewUserDataPath: opts.WebviewDataDir,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: opts.InstanceID,
			// The data of the second launch (its arguments, a paratech: link among them) is ignored: a second launch
			// only brings the main window forward (ADR-0013).
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				onSecondInstance()
			},
		},
	}
}

// withMedia puts the player of the recordings in front of the asset handler; until the core is open (setSource) the
// recordings are not found.
func withMedia(opts *application.Options, log *slog.Logger) *mediaHandler {
	media := newMediaHandler(opts.Assets.Handler, log)
	opts.Assets.Handler = media
	return media
}

// asfwAny is ASFW_ANY of AllowSetForegroundWindow.
const asfwAny = ^uint32(0)

// allowForegroundHandOff lets another process take the foreground until the next user input, if this process may set it
// at all; otherwise the call fails and changes nothing. Best effort: the result is ignored.
func allowForegroundHandOff() {
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow").Call(uintptr(asfwAny))
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

// newTray adds the tray icon; "Show recording popup" calls offer, which goes through the core like a lesson prompt.
func newTray(app *application.App, u *windowsUI, name string, offer func()) {
	menu := application.NewMenu()
	menu.Add(menuOpen).OnClick(func(*application.Context) { _ = u.showMain() })
	menu.Add(menuShowPopup).OnClick(func(*application.Context) { offer() })
	menu.AddSeparator()
	menu.Add(menuQuit).OnClick(func(*application.Context) { app.Quit() })

	tray := app.SystemTray.New()
	// One icon for the light and the dark taskbar: Wails uses it for both when no dark one is set.
	tray.SetIcon(appIcon())
	tray.SetTooltip(name)
	tray.SetMenu(menu)
	tray.OnClick(func() { _ = u.showMain() })
}

func (u *windowsUI) showPopup() error {
	// Every offer starts at the normal size; the page asks to grow again if its content needs it.
	u.placePopup(popupHeight)

	// Emitted while still hidden so the page starts re-reading the prompt before it appears; the hidden WebView still
	// runs script. Delivery and the page's call are asynchronous, so this narrows the stale frame, it cannot rule it out.
	u.app.Event.Emit(PromptShownEvent)

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

func (u *windowsUI) fitPopup(height int) (int, error) {
	return u.placePopup(height), nil
}

// placePopup sizes the popup for content height want and puts it in the bottom-right corner of the primary work
// area, in one move. Wails' SetBounds uses SWP_NOACTIVATE, so this never takes focus. It returns the height set.
func (u *windowsUI) placePopup(want int) int {
	scr := u.app.Screen.GetPrimary()
	if scr == nil {
		u.popup.SetSize(popupWidth, popupHeight)
		return popupHeight
	}
	wa := scr.WorkArea
	b := popupBounds(rect{X: wa.X, Y: wa.Y, Width: wa.Width, Height: wa.Height}, want, popupMargin)
	u.popup.SetBounds(application.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height})
	return b.Height
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
