//go:build windows

package pdf

import (
	"context"
	"errors"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Vtable slots counted from WebView2.h; every interface starts with the three IUnknown slots, and an interface
// «N» continues the slots of the one it extends. Wails' generated ICoreWebView2_7 vtable omits the inherited
// methods, so its layout is not usable here.
const (
	// ICoreWebView2Environment (8 slots) → 2 (+1) → 3 (+2) → 4 (+1) → 5 (+2) → 6.
	slotEnvCreateController   = 3
	slotEnv6CreatePrintConfig = 14

	// ICoreWebView2Controller.
	slotControllerClose       = 24
	slotControllerCoreWebView = 25

	// ICoreWebView2 (61 slots) → _2 (+7) → _3 (+5) → _4 (+4) → _5 (+2) → _6 (+1) → _7.
	slotWebViewSettings                = 3
	slotWebViewNavigateToString        = 6
	slotWebViewAddNavigationStarting   = 7
	slotWebViewAddNavigationCompleted  = 15
	slotWebViewAddFrameNavigationStart = 17
	slotWebView7PrintToPdf             = 80

	// ICoreWebView2Settings.
	slotSettingsPutScriptEnabled     = 4
	slotSettingsPutWebMessageEnabled = 6
	slotSettingsPutDevToolsEnabled   = 12
	slotSettingsPutContextMenus      = 14
	slotSettingsPutHostObjects       = 16

	// ICoreWebView2NavigationStartingEventArgs.
	slotStartingPutCancel       = 8
	slotStartingGetNavigationID = 9

	// ICoreWebView2NavigationCompletedEventArgs.
	slotCompletedGetIsSuccess    = 3
	slotCompletedGetNavigationID = 5

	// ICoreWebView2PrintSettings.
	slotPrintPutOrientation     = 4
	slotPrintPutMarginTop       = 12
	slotPrintPutMarginBottom    = 14
	slotPrintPutMarginLeft      = 16
	slotPrintPutMarginRight     = 18
	slotPrintPutBackgrounds     = 20
	slotPrintPutHeaderAndFooter = 24
	printOrientationPortrait    = 0

	runtimeTypeInstalled          = 0
	createEnvironmentInternalFlag = 1 // first argument of the internal entry point, as the official loader passes it
)

// IIDs checked against WebView2.h (vendored in github.com/webview/webview_go, libs/mswebview2/include) and, for
// the environment ones, github.com/wailsapp/go-webview2.
var (
	iidEnvironmentCompletedHandler = windows.GUID{Data1: 0x4e8a3389, Data2: 0xc9d8, Data3: 0x4bd2,
		Data4: [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d}}
	iidControllerCompletedHandler = windows.GUID{Data1: 0x6c4819f3, Data2: 0xc9b7, Data3: 0x4260,
		Data4: [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c}}
	iidNavigationStartingHandler = windows.GUID{Data1: 0x9adbe429, Data2: 0xf36d, Data3: 0x432b,
		Data4: [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
	iidNavigationCompletedHandler = windows.GUID{Data1: 0xd33a35bf, Data2: 0x1c49, Data3: 0x4f98,
		Data4: [8]byte{0x93, 0xab, 0x00, 0x6e, 0x05, 0x33, 0xfe, 0x1c}}
	iidPrintToPdfCompletedHandler = windows.GUID{Data1: 0xccf1ef04, Data2: 0xfd8e, Data3: 0x4d5f,
		Data4: [8]byte{0xb2, 0xde, 0x09, 0x83, 0xe4, 0x1b, 0x8c, 0x36}}
	iidEnvironment6 = windows.GUID{Data1: 0xe59ee362, Data2: 0xacbd, Data3: 0x4857,
		Data4: [8]byte{0x9a, 0x8e, 0xd3, 0x64, 0x4d, 0x94, 0x59, 0xa9}}
	iidWebView7 = windows.GUID{Data1: 0x79c24d83, Data2: 0x09a3, Data3: 0x45ae,
		Data4: [8]byte{0x94, 0x18, 0x48, 0x7f, 0x32, 0xa5, 0x87, 0x40}}
)

// session is one print: environment, hidden controller, navigation and PrintToPdf on one STA thread.
type session struct {
	runtime       webviewRuntime
	userDataDir   string
	html          string
	pdfPath       string
	enableScripts bool

	holdBeforePrint time.Duration
}

// run owns a locked OS thread for the whole call: WebView2 objects belong to the STA thread that created them
// and report back only through its message loop.
func (s *session) run(ctx context.Context) error {
	// No UnlockOSThread: the goroutine ends locked, so Go discards the thread together with its COM apartment.
	runtime.LockOSThread()

	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		// S_FALSE: COM is already initialized as STA on this thread; it still needs CoUninitialize.
		if !errors.Is(err, syscall.Errno(1)) {
			return failure(opThread, uintptr(errnoValue(err)), "CoInitializeEx")
		}
	}
	defer windows.CoUninitialize()

	if err := loadUser32(); err != nil {
		return failure(opWindow, 0, "user32")
	}
	hwnd, err := createHostWindow()
	if err != nil {
		return failure(opWindow, uintptr(errnoValue(err)), "CreateWindowEx")
	}
	defer destroyWindow(hwnd)

	env, err := s.createEnvironment(ctx)
	defer env.release()
	if err != nil {
		return err
	}

	controller, err := s.createController(ctx, env, hwnd)
	defer func() {
		if controller != nil {
			_ = controller.call(slotControllerClose)
			controller.release()
		}
	}()
	if err != nil {
		return err
	}

	webview, hr := controller.getObject(slotControllerCoreWebView)
	defer webview.release()
	if webview == nil {
		return failure(opController, hr, "get_CoreWebView2")
	}

	if err := s.configure(webview); err != nil {
		return err
	}
	if err := s.navigate(ctx, webview); err != nil {
		return err
	}
	if err := s.hold(ctx); err != nil {
		return err
	}
	return s.printToPdf(ctx, env, webview)
}

func (s *session) createEnvironment(ctx context.Context) (*comObject, error) {
	dll, err := windows.LoadDLL(s.runtime.dll)
	if err != nil {
		return nil, &opError{op: opLoadRuntime, err: ErrUnsupported, detail: "load client dll"}
	}
	// The module stays loaded: Windows reuses it for the next call with the same path.
	proc, err := dll.FindProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return nil, &opError{op: opLoadRuntime, err: ErrUnsupported, detail: "no environment entry point"}
	}

	folder, err := windows.UTF16PtrFromString(s.userDataDir)
	if err != nil {
		return nil, failure(opEnvironment, 0, "user data folder")
	}

	var (
		env   *comObject
		envHR uintptr
		done  bool
	)
	handler := newResultHandler(iidEnvironmentCompletedHandler, func(hr uintptr, obj *comObject) {
		done, envHR = true, hr
		if obj != nil && !failed(hr) {
			obj.addRef()
			env = obj
		}
	})
	defer handlerRelease(handler)

	// No options object: defaults are what we need, and the user data folder is a plain argument.
	// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS and similar env/registry overrides are not neutralised here (the app
	// clears them process-wide through Wails); per the WebView2 docs they take precedence over an options object too.
	if hr := createEnvironment(proc, createEnvironmentInternalFlag, runtimeTypeInstalled, folder, handler); failed(hr) {
		return nil, failure(opEnvironment, hr, "create")
	}

	if err := pumpUntil(ctx, func() bool { return done }); err != nil {
		settle(func() bool { return done })
		return env, err
	}
	if env == nil {
		return nil, failure(opEnvironment, envHR, "completed")
	}
	return env, nil
}

func (s *session) createController(ctx context.Context, env *comObject, hwnd uintptr) (*comObject, error) {
	var (
		controller *comObject
		ctrlHR     uintptr
		done       bool
	)
	handler := newResultHandler(iidControllerCompletedHandler, func(hr uintptr, obj *comObject) {
		done, ctrlHR = true, hr
		if obj != nil && !failed(hr) {
			obj.addRef()
			controller = obj
		}
	})
	defer handlerRelease(handler)

	if hr := env.callWithHandler(slotEnvCreateController, hwnd, handler); failed(hr) {
		return nil, failure(opController, hr, "create")
	}

	if err := pumpUntil(ctx, func() bool { return done }); err != nil {
		settle(func() bool { return done })
		return controller, err
	}
	if controller == nil {
		return nil, failure(opController, ctrlHR, "completed")
	}
	return controller, nil
}

// configure turns scripts and host bridges off; the page is static print HTML.
func (s *session) configure(webview *comObject) error {
	settings, hr := webview.getObject(slotWebViewSettings)
	if settings == nil {
		return failure(opSettings, hr, "get_Settings")
	}
	defer settings.release()

	puts := []struct {
		slot  int
		value bool
	}{
		{slotSettingsPutScriptEnabled, s.enableScripts},
		{slotSettingsPutWebMessageEnabled, false},
		{slotSettingsPutDevToolsEnabled, false},
		{slotSettingsPutContextMenus, false},
		{slotSettingsPutHostObjects, false},
	}
	for _, p := range puts {
		if hr := settings.putBool(p.slot, p.value); failed(hr) {
			return failure(opSettings, hr, "put")
		}
	}
	return nil
}

// navigate loads the HTML and waits for its navigation to finish. Only that navigation may start: any other
// main-frame navigation and every frame navigation is cancelled.
func (s *session) navigate(ctx context.Context, webview *comObject) error {
	html, err := windows.UTF16PtrFromString(s.html)
	if err != nil {
		return failure(opNavigate, 0, "html")
	}

	var (
		allowOne   = true
		ownID      uint64
		ownStarted bool
		done       bool
		success    bool
	)
	// A renderer crash is noticed only at the ctx deadline (no ProcessFailed handler); the caller sets the timeout.
	starting := newEventHandler(iidNavigationStartingHandler, func(args *comObject) {
		if !allowOne {
			_ = args.putBool(slotStartingPutCancel, true)
			return
		}
		allowOne = false
		id, hr := args.getUint64(slotStartingGetNavigationID)
		if failed(hr) {
			// Without the id our completion cannot be recognised; fail now instead of waiting for the deadline.
			done, success = true, false
			return
		}
		ownID, ownStarted = id, true
	})
	defer handlerRelease(starting)

	frames := newEventHandler(iidNavigationStartingHandler, func(args *comObject) {
		_ = args.putBool(slotStartingPutCancel, true)
	})
	defer handlerRelease(frames)

	completed := newEventHandler(iidNavigationCompletedHandler, func(args *comObject) {
		id, hr := args.getUint64(slotCompletedGetNavigationID)
		if failed(hr) || !ownStarted || id != ownID {
			return
		}
		ok, hr := args.getBool(slotCompletedGetIsSuccess)
		done, success = true, ok && !failed(hr)
	})
	defer handlerRelease(completed)

	subscriptions := []struct {
		slot    int
		handler *comHandler
	}{
		{slotWebViewAddNavigationStarting, starting},
		{slotWebViewAddFrameNavigationStart, frames},
		{slotWebViewAddNavigationCompleted, completed},
	}
	for _, sub := range subscriptions {
		if hr := webview.addHandler(sub.slot, sub.handler); failed(hr) {
			return failure(opSubscribe, hr, "")
		}
	}

	if hr := webview.putString(slotWebViewNavigateToString, html); failed(hr) {
		return failure(opNavigate, hr, "NavigateToString")
	}
	if err := pumpUntil(ctx, func() bool { return done }); err != nil {
		return err
	}
	if !success {
		return failure(opNavigate, 0, "navigation failed")
	}
	return nil
}

// hold keeps the message loop running for holdBeforePrint (tests only; zero in production).
func (s *session) hold(ctx context.Context) error {
	if s.holdBeforePrint <= 0 {
		return nil
	}
	holdCtx, cancel := context.WithTimeout(ctx, s.holdBeforePrint)
	defer cancel()
	_ = pumpUntil(holdCtx, func() bool { return false })
	return ctx.Err()
}

func (s *session) printToPdf(ctx context.Context, env, webview *comObject) error {
	env6, hr := env.queryInterface(&iidEnvironment6)
	if env6 == nil {
		return &opError{op: opPrintSetup, hr: hr, err: ErrUnsupported, detail: "no ICoreWebView2Environment6"}
	}
	defer env6.release()

	webview7, hr := webview.queryInterface(&iidWebView7)
	if webview7 == nil {
		return &opError{op: opPrintSetup, hr: hr, err: ErrUnsupported, detail: "no ICoreWebView2_7"}
	}
	defer webview7.release()

	settings, hr := env6.getObject(slotEnv6CreatePrintConfig)
	if settings == nil {
		return failure(opPrintSetup, hr, "CreatePrintSettings")
	}
	defer settings.release()

	if err := configurePrint(settings); err != nil {
		return err
	}

	path, err := windows.UTF16PtrFromString(s.pdfPath)
	if err != nil {
		return failure(opPrint, 0, "path")
	}

	var (
		done    bool
		success bool
		doneHR  uintptr
	)
	handler := newPrintHandler(iidPrintToPdfCompletedHandler, func(hr uintptr, ok bool) {
		done, doneHR, success = true, hr, ok && !failed(hr)
	})
	defer handlerRelease(handler)

	if hr := webview7.printToPdf(slotWebView7PrintToPdf, path, settings, handler); failed(hr) {
		return failure(opPrint, hr, "PrintToPdf")
	}
	if err := pumpUntil(ctx, func() bool { return done }); err != nil {
		return err
	}
	if !success {
		return failure(opPrint, doneHR, "not successful")
	}
	return nil
}

// configurePrint: portrait, backgrounds on, no browser header and footer, zero margins because the HTML sets
// them with @page. On arm64 the doubles cannot be passed, so the runtime's default margins stay.
func configurePrint(settings *comObject) error {
	if hr := settings.call(slotPrintPutOrientation, printOrientationPortrait); failed(hr) {
		return failure(opPrintSetup, hr, "orientation")
	}
	if hr := settings.putBool(slotPrintPutBackgrounds, true); failed(hr) {
		return failure(opPrintSetup, hr, "backgrounds")
	}
	if hr := settings.putBool(slotPrintPutHeaderAndFooter, false); failed(hr) {
		return failure(opPrintSetup, hr, "header and footer")
	}
	for _, slot := range []int{slotPrintPutMarginTop, slotPrintPutMarginBottom, slotPrintPutMarginLeft,
		slotPrintPutMarginRight} {
		hr, ok := settings.putDouble(slot, 0)
		if !ok {
			break
		}
		if failed(hr) {
			return failure(opPrintSetup, hr, "margins")
		}
	}
	return nil
}

// settle waits a bounded time for a creation that ctx abandoned. Its result must still arrive to be released:
// an environment that is never released keeps its browser process (and the profile folder) alive until the app
// exits.
func settle(done func() bool) {
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	_ = pumpUntil(ctx, done)
}

func errnoValue(err error) syscall.Errno {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	return 0
}
