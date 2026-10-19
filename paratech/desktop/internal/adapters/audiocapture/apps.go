package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Operations of Apps, for the errors.
const (
	opList  = "list the open apps"
	opFind  = "find the app"
	opCheck = "check the capture target"
)

// Bounds of one enumeration of the open apps (TASK-059).
const (
	appsTimeout   = 2 * time.Second // the whole List, Find or Same
	iconBudget    = time.Second     // icons not cached yet are read only this long into a List
	maxProcesses  = 4096
	maxWindows    = 1024 // visible top-level windows
	maxSessions   = 256  // audio sessions over all output devices
	maxApps       = 64   // entries of the picker
	maxTitleBytes = 256
	maxTreeDepth  = 64 // parent links walked from one process
	maxIconCache  = 128
	maxNameCache  = 256
)

// Audio session states (AudioSessionState, audiosessiontypes.h); 0 is inactive.
const (
	sessionActive  = 1
	sessionExpired = 2
)

var (
	// ErrAppList means the open apps could not be listed or checked (Windows refused, or it took too long).
	ErrAppList = errors.New("audiocapture: cannot list the open apps")

	// ErrAppNotRunning means the chosen app does not run in this Windows session. It wraps
	// recording.ErrSourceNotRunning.
	ErrAppNotRunning = fmt.Errorf("audiocapture: %w", recording.ErrSourceNotRunning)
)

// internalApp reports a Windows shell host with windows of its own that is no lecture app (OBS skips them too).
// applicationframehost.exe hosts UWP apps; the app's own window inside the frame is listed instead.
func internalApp(app shared.AppName) bool {
	switch app {
	case "applicationframehost.exe", "shellexperiencehost.exe", "startmenuexperiencehost.exe", "searchapp.exe",
		"searchui.exe", "searchhost.exe", "textinputhost.exe", "lockapp.exe", "peopleexperiencehost.exe",
		"cortana.exe", "gamebar.exe", "tabtip.exe", "systemsettings.exe":
		return true
	}
	return strings.Contains(string(app), "windowsinternal")
}

// Window styles (winuser.h).
const (
	wsChild        = 0x40000000 // WS_CHILD
	wsExToolWindow = 0x00000080 // WS_EX_TOOLWINDOW
)

// procInfo is one process of a snapshot.
type procInfo struct {
	pid, parent uint32
	exe         string // image file name as the snapshot reports it ("Chrome.exe")
	created     uint64 // creation time (FILETIME, 100 ns since 1601); 0 when unknown
	session     uint32 // Windows session
	sessionOK   bool
}

// windowInfo is one visible top-level window, topmost first.
type windowInfo struct {
	owner          uint32 // PID of the owner; for a UWP frame the PID of the app inside it
	title          string // untrusted text
	visible        bool
	style, exStyle uint32
}

// sessionInfo is one audio session of an output device.
type sessionInfo struct {
	pid      uint32
	state    uint32
	system   bool // the system sounds session
	muted    bool
	volume   float32
	volumeOK bool
	peak     float32 // peak meter of the last processing period, 0..1
	peakOK   bool
}

// snapshot is the raw state the platform reads in one go.
type snapshot struct {
	self, selfSession uint32
	procs             []procInfo
	windows           []windowInfo
	sessions          []sessionInfo
	sessionsErr       error // the sessions could not be read; the apps are listed without them
}

// procIdentity is what the platform reads about one running process.
type procIdentity struct {
	alive   bool
	image   string // full path of the executable
	session uint32
	created uint64
}

// appPlatform reads the raw state of this OS. Its calls block; Apps bounds them in time.
type appPlatform interface {
	// snapshot reads the processes and the visible top-level windows, and the audio sessions when sessions is true.
	snapshot(sessions bool) (snapshot, error)

	// inspect opens the process pid; an unknown PID is not alive and no error.
	inspect(pid uint32) (procIdentity, error)

	// icon returns a small PNG of the icon of the executable at path, nil when there is none.
	icon(path string) []byte

	// describe returns FileDescription and ProductName of the version resource of the executable at path (TASK-082);
	// empty when there is none. Untrusted text.
	describe(path string) (description, product string)
}

// candidate is an app found in a snapshot: its picker entry and the root of its process tree.
type candidate struct {
	app  values.AudioApp
	root procInfo
}

// tree links the processes of a snapshot to their parents.
type tree struct {
	byPID    map[uint32]procInfo
	children map[uint32][]uint32
}

func newTree(procs []procInfo) tree {
	t := tree{byPID: make(map[uint32]procInfo, len(procs)), children: map[uint32][]uint32{}}
	for _, p := range procs {
		if p.pid != 0 {
			t.byPID[p.pid] = p
		}
	}
	for _, p := range t.byPID {
		if _, ok := t.parentOf(p); ok {
			t.children[p.parent] = append(t.children[p.parent], p.pid)
		}
	}
	return t
}

// parentOf returns the parent of p. A parent PID may have been reused by a process started after p (Windows keeps
// only the number), so a parent must be older than its child; unknown creation times link nothing.
func (t tree) parentOf(p procInfo) (procInfo, bool) {
	q, ok := t.byPID[p.parent]
	if !ok || q.pid == p.pid || q.created == 0 || p.created == 0 || q.created > p.created {
		return procInfo{}, false
	}
	return q, true
}

// subtree returns pid and its descendants.
func (t tree) subtree(pid uint32) map[uint32]bool {
	out := map[uint32]bool{pid: true}
	queue := []uint32{pid}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range t.children[next] {
			if !out[c] {
				out[c] = true
				queue = append(queue, c)
			}
		}
	}
	return out
}

// sameExeRoot walks up from p while the parent runs the same executable: the browser's main process, not a renderer.
func (t tree) sameExeRoot(p procInfo) procInfo {
	for range maxTreeDepth {
		q, ok := t.parentOf(p)
		if !ok || !strings.EqualFold(q.exe, p.exe) {
			break
		}
		p = q
	}
	return p
}

// ancestorIn walks up from pid (itself included) to the first process in set.
func (t tree) ancestorIn(pid uint32, set map[uint32]int) (int, bool) {
	p, ok := t.byPID[pid]
	for range maxTreeDepth {
		if !ok {
			return 0, false
		}
		if i, in := set[p.pid]; in {
			return i, true
		}
		p, ok = t.parentOf(p)
	}
	return 0, false
}

// listable reports whether a window stands for an app, as OBS lists windows: visible, not a child, not a tool window.
func (w windowInfo) listable() bool {
	return w.visible && w.owner != 0 && w.style&wsChild == 0 && w.exStyle&wsExToolWindow == 0
}

// appsOf builds at most limit apps from a snapshot: one per executable, from its topmost window, without this app's
// own process tree (its WebView2 included) and without processes of another Windows session.
func appsOf(s snapshot, limit int) []candidate {
	t := newTree(s.procs)
	own := t.subtree(s.self)
	var out []candidate
	seen := map[shared.AppName]bool{}
	for _, w := range s.windows {
		if len(out) == limit {
			break
		}
		if !w.listable() || own[w.owner] {
			continue
		}
		p, ok := t.byPID[w.owner]
		if !ok || !p.sessionOK || p.session != s.selfSession {
			continue
		}
		app, err := shared.ParseAppName(p.exe)
		title := cleanTitle(w.title)
		if err != nil || seen[app] || internalApp(app) || (app == "explorer.exe" && title == "") {
			continue
		}
		seen[app] = true
		root := t.sameExeRoot(p)
		out = append(out, candidate{root: root, app: values.AudioApp{App: app, Title: title, PID: root.pid}})
	}
	markSessions(t, out, s.sessions)
	return out
}

// markSessions sets Playing, Sounding and Muted from the audio sessions. A session belongs to an app when its process
// is in the app's tree: Chromium plays through a child process ("Audio Service"), so session PIDs are walked up.
// Sounding uses the test of the healthy-but-mute detector (sessionInfo.audible): an active, unmuted session whose
// meter shows more than peakFloor now. The meter is one reading, so the low floor catches quiet speech; an idle app
// reads 0.
func markSessions(t tree, apps []candidate, sessions []sessionInfo) {
	roots := make(map[uint32]int, len(apps))
	for i, c := range apps {
		roots[c.root.pid] = i
	}
	for _, ss := range sessions {
		if ss.system || ss.state == sessionExpired || ss.pid == 0 {
			continue
		}
		i, ok := t.ancestorIn(ss.pid, roots)
		// Most apps are children of the shell: only explorer's own sessions are explorer's.
		if !ok || (apps[i].app.App == "explorer.exe" && ss.pid != apps[i].root.pid) {
			continue
		}
		if ss.state == sessionActive {
			apps[i].app.Playing = true
		}
		if ss.audible() {
			apps[i].app.Sounding = true
		}
		if ss.muted || (ss.volumeOK && ss.volume <= 0) {
			apps[i].app.Muted = true
		}
	}
}

// cleanTitle turns a window title into one bounded line. Format characters (bidi overrides, zero-width ones) are
// dropped, so a window cannot make its name look like another one.
func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || r == utf8.RuneError:
			return ' '
		}
		return r
	}, s)
	if len(s) > maxTitleBytes {
		cut := maxTitleBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return strings.TrimSpace(s)
}

// imageApp is the app name of an executable path ("C:\...\Zoom.exe" -> "zoom.exe").
func imageApp(path string) shared.AppName {
	base := path[strings.LastIndexAny(path, `\/`)+1:]
	app, err := shared.ParseAppName(base)
	if err != nil {
		return ""
	}
	return app
}

// ProcessIdentity names one running process of an app. Windows reuses PIDs, so the creation time tells a process
// from a later one with the same PID.
type ProcessIdentity struct {
	App     shared.AppName
	PID     uint32
	Created uint64 // FILETIME, 100 ns since 1601
}

// Apps lists the open apps for the source picker and finds the process to capture for a chosen app (TASK-059). It
// satisfies the AudioApps port of the recording prompt and is the AppFinder of the Recorder. Every call is bounded
// in time and the calls of one Apps run one at a time. PlatformApps is the implementation of this OS.
type Apps struct {
	platform appPlatform
	log      *slog.Logger
	now      func() time.Time
	timeout  time.Duration
	gate     chan struct{} // one platform call at a time; held until a timed-out call ends

	mu    sync.Mutex
	icons map[string][]byte // by executable path; only icons read, so a failed read is tried again by a later List
	names map[string]appVersion
}

// appVersion is what the version resource of an executable says about it; empty when it says nothing.
type appVersion struct{ description, product string }

// PlatformApps returns the apps of this OS: on 64-bit Windows the visible windows and audio sessions; elsewhere every
// call fails with an error wrapping ErrUnavailable (recording.ErrCaptureUnavailable).
func PlatformApps(log *slog.Logger) *Apps { return newApps(platformApps(), log) }

func newApps(p appPlatform, log *slog.Logger) *Apps {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Apps{
		platform: p, log: log, now: time.Now, timeout: appsTimeout, gate: make(chan struct{}, 1), icons: map[string][]byte{},
		names: map[string]appVersion{},
	}
}

// List returns the apps with a visible window in this Windows session except this app, one per executable, topmost
// first. Playing, Sounding and Muted come from the audio sessions; without them (Windows refused) the apps are still
// listed. Icons and version resources are cached per executable; an icon that cannot be read, or is not read within
// iconBudget, is nil until a later List reads it. Name is never empty (values.DisplayName): the known name, the
// version resource read within iconBudget, else the executable name.
func (a *Apps) List(ctx context.Context) ([]values.AudioApp, error) {
	return bounded(ctx, a, opList, func() ([]values.AudioApp, error) {
		began := a.now()
		snap, err := a.platform.snapshot(true)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAppList, err)
		}
		if snap.sessionsErr != nil {
			a.log.Warn("audio sessions could not be read; the apps are listed without them",
				slog.String("error", shortText(snap.sessionsErr.Error())))
		}
		found := appsOf(snap, maxApps)
		out := make([]values.AudioApp, 0, len(found))
		for _, c := range found {
			read := a.now().Sub(began) < iconBudget
			c.app.IconPNG = a.iconOf(c.root.pid, read)
			c.app.Name = a.nameOf(c.app.App, c.root.pid, read)
			out = append(out, c.app)
		}
		return out, nil
	})
}

// Find returns the process to capture for app: the root of the process tree of its topmost visible window in this
// Windows session, outside this app's tree, checked right before it is returned (it runs, its executable is app, its
// session is ours). An app that does not run yields an error wrapping ErrAppNotRunning; a failure to look or to check
// the process wraps ErrAppList. The process may still exit right after; the Recorder watches it (Same).
func (a *Apps) Find(ctx context.Context, app shared.AppName) (ProcessIdentity, error) {
	if err := app.Validate(); err != nil {
		return ProcessIdentity{}, fmt.Errorf("%w: %w", ErrAppNotRunning, err)
	}
	return bounded(ctx, a, opFind, func() (ProcessIdentity, error) {
		snap, err := a.platform.snapshot(false)
		if err != nil {
			return ProcessIdentity{}, fmt.Errorf("%w: %w", ErrAppList, err)
		}
		var root procInfo
		for _, c := range appsOf(snap, maxWindows) {
			if c.app.App == app {
				root = c.root
				break
			}
		}
		if root.pid == 0 {
			return ProcessIdentity{}, fmt.Errorf("%w: %s has no window", ErrAppNotRunning, app)
		}
		id, err := a.platform.inspect(root.pid)
		if err != nil {
			return ProcessIdentity{}, fmt.Errorf("%w: %s: process %d cannot be checked: %w", ErrAppList, app, root.pid, err)
		}
		switch {
		case !id.alive:
			return ProcessIdentity{}, fmt.Errorf("%w: %s: process %d exited", ErrAppNotRunning, app, root.pid)
		case imageApp(id.image) != app:
			return ProcessIdentity{}, fmt.Errorf("%w: %s: process %d runs another program", ErrAppNotRunning, app, root.pid)
		case id.session != snap.selfSession:
			return ProcessIdentity{}, fmt.Errorf("%w: %s: process %d is in another Windows session", ErrAppNotRunning, app, root.pid)
		case root.created != 0 && id.created != root.created:
			return ProcessIdentity{}, fmt.Errorf("%w: %s: process %d was replaced", ErrAppNotRunning, app, root.pid)
		}
		return ProcessIdentity{App: app, PID: root.pid, Created: id.created}, nil
	})
}

// Same reports whether id still names the same running process: alive, with the same creation time and executable.
// A PID reused by another program is not the same.
func (a *Apps) Same(ctx context.Context, id ProcessIdentity) (bool, error) {
	return bounded(ctx, a, opCheck, func() (bool, error) {
		cur, err := a.platform.inspect(id.PID)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrAppList, err)
		}
		return cur.alive && cur.created == id.Created && imageApp(cur.image) == id.App, nil
	})
}

// iconOf returns the cached icon of the executable of pid, reading it first when read is true.
func (a *Apps) iconOf(pid uint32, read bool) []byte {
	id, err := a.platform.inspect(pid)
	if err != nil || id.image == "" {
		return nil
	}
	a.mu.Lock()
	icon, ok := a.icons[id.image]
	a.mu.Unlock()
	if ok || !read {
		return icon
	}
	icon = a.platform.icon(id.image)
	if icon == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.icons) >= maxIconCache {
		clear(a.icons)
	}
	a.icons[id.image] = icon
	return icon
}

// nameOf returns the readable name of app, reading the version resource of the executable of pid first when read is
// true and it is not cached. A known name needs no read.
func (a *Apps) nameOf(app shared.AppName, pid uint32, read bool) string {
	if name, ok := values.KnownAppName(app); ok {
		return name
	}
	var v appVersion
	if id, err := a.platform.inspect(pid); err == nil && id.image != "" {
		a.mu.Lock()
		cached, ok := a.names[id.image]
		a.mu.Unlock()
		switch {
		case ok:
			v = cached
		case read:
			v.description, v.product = a.platform.describe(id.image)
			a.mu.Lock()
			if len(a.names) >= maxNameCache {
				clear(a.names)
			}
			a.names[id.image] = v // also when empty: an executable without a version resource keeps none
			a.mu.Unlock()
		}
	}
	return values.DisplayName(app, v.description, v.product)
}

// bounded runs f on its own goroutine, one call of a at a time, and gives up after a.timeout or when ctx ends. A
// call that gave up still runs to its end (Windows calls cannot be cancelled) and holds the gate until then, so
// calls never pile up. A panic of f is returned as an error.
func bounded[T any](ctx context.Context, a *Apps, op string, f func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	var zero T
	if err := ctx.Err(); err != nil { // select picks at random when the gate is free too
		return zero, fmt.Errorf("%w: %s: %w", ErrAppList, op, err)
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	select {
	case a.gate <- struct{}{}:
	case <-ctx.Done():
		return zero, fmt.Errorf("%w: %s: %w", ErrAppList, op, ctx.Err())
	}

	done := make(chan result, 1)
	go func() {
		defer func() { <-a.gate }()
		defer func() {
			if p := recover(); p != nil {
				done <- result{err: fmt.Errorf("%w: %s: unexpected failure: %v", ErrAppList, op, p)}
			}
		}()
		v, err := f()
		done <- result{v: v, err: err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		return zero, fmt.Errorf("%w: %s: %w", ErrAppList, op, ctx.Err())
	}
}
