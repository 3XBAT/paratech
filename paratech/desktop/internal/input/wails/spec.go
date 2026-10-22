package wails

// Window names; Wails looks windows up by name.
const (
	mainWindowName  = "main"
	popupWindowName = "popup"
)

// Popup geometry in device-independent pixels.
const (
	popupWidth  = 360
	popupHeight = 300 // title, lesson time, source picker, advice lines, an error line and three buttons
	popupMargin = 16
	// popupMaxHeight bounds the growth for a start error; beyond it the page scrolls its text area.
	popupMaxHeight = 440
)

// rect is a screen rectangle in device-independent pixels.
type rect struct {
	X, Y, Width, Height int
}

// windowSpec is the platform-neutral description of a window; the Windows file maps it to Wails options.
type windowSpec struct {
	Name   string
	Title  string
	URL    string
	Width  int
	Height int

	Frameless     bool
	AlwaysOnTop   bool
	Hidden        bool
	DisableResize bool
	// NoActivate asks Windows not to activate the window when it is shown or clicked (WS_EX_NOACTIVATE).
	NoActivate bool
	// HiddenOnTaskbar keeps the window off the taskbar (WS_EX_TOOLWINDOW).
	HiddenOnTaskbar bool
	// HideOnClose turns the close button into "hide", so the app keeps running in the tray.
	HideOnClose bool
	// FileDrop lets files be dropped on the window; Go imports them and emits LibraryDropEvent.
	FileDrop bool
}

func mainWindowSpec(title string) windowSpec {
	return windowSpec{
		Name:        mainWindowName,
		Title:       title,
		URL:         mainPage,
		Width:       1024,
		Height:      720,
		HideOnClose: true,
		FileDrop:    true,
	}
}

// popupWindowSpec is the recording prompt (PRD section 6.1): small, frameless, above other windows, created hidden
// and shown without taking focus from the app the student is in (AC-01).
func popupWindowSpec(title string) windowSpec {
	return windowSpec{
		Name:            popupWindowName,
		Title:           title,
		URL:             popupPage,
		Width:           popupWidth,
		Height:          popupHeight,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		NoActivate:      true,
		HiddenOnTaskbar: true,
		HideOnClose:     true,
	}
}

// popupOrigin places a w x h popup in the bottom-right corner of the work area (next to the tray), keeping it
// inside the work area when the area is smaller than the popup plus the margin.
func popupOrigin(work rect, w, h, margin int) (x, y int) {
	x = max(work.X+work.Width-w-margin, work.X)
	y = max(work.Y+work.Height-h-margin, work.Y)
	return x, y
}

// popupFitHeight is the popup height for content that needs want: at least popupHeight, at most popupMaxHeight and the
// work area minus both margins. A work area too small for even popupHeight keeps popupHeight, as before the growth.
func popupFitHeight(want int, work rect, margin int) int {
	limit := min(popupMaxHeight, work.Height-2*margin)
	return max(popupHeight, min(want, limit))
}

// popupBounds is where a popup of content height want goes: fitted, in the bottom-right corner of the work area.
func popupBounds(work rect, want, margin int) rect {
	h := popupFitHeight(want, work, margin)
	x, y := popupOrigin(work, popupWidth, h, margin)
	return rect{X: x, Y: y, Width: popupWidth, Height: h}
}
