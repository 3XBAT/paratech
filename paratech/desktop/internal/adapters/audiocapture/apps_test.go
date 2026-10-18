package audiocapture

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// The pure logic of the source picker on fake snapshots: no test here reads real windows or audio sessions.

const (
	ourPID     = 100
	ourSession = 1
)

// proc is a process of session 1 created at created.
func proc(pid, parent uint32, exe string, created uint64) procInfo {
	return procInfo{pid: pid, parent: parent, exe: exe, created: created, session: ourSession, sessionOK: true}
}

func win(owner uint32, title string) windowInfo {
	return windowInfo{owner: owner, title: title, visible: true}
}

// desktop is a typical session: the shell, this app with its WebView2, Chrome with a renderer and its audio service,
// Zoom, and a process of another Windows session.
func desktop() snapshot {
	return snapshot{
		self: ourPID, selfSession: ourSession,
		procs: []procInfo{
			proc(10, 4, "explorer.exe", 10),
			proc(ourPID, 10, "la-desktop.exe", 100),
			proc(101, ourPID, "msedgewebview2.exe", 101),
			proc(200, 10, "chrome.exe", 200),
			proc(201, 200, "chrome.exe", 201), // renderer
			proc(202, 200, "chrome.exe", 202), // audio service
			proc(300, 10, "Zoom.exe", 300),
			{pid: 400, parent: 4, exe: "teams.exe", created: 400, session: 2, sessionOK: true},
			{pid: 500, parent: 10, exe: "unknown.exe", created: 500},
		},
		windows: []windowInfo{
			win(101, "paratech"), // our WebView2
			win(ourPID, "paratech"),
			win(201, "Lecture - Chrome"),
			win(300, "Zoom Meeting"),
			win(200, "Second Chrome window"),
			win(400, "Teams of another user"),
			win(500, "No session"),
			win(10, ""),
		},
	}
}

func appNames(cs []candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.app.App.String())
	}
	return out
}

func TestAppsOfListsOneEntryPerApp(t *testing.T) {
	got := appsOf(desktop(), maxApps)
	if names := appNames(got); !slices.Equal(names, []string{"chrome.exe", "zoom.exe"}) {
		t.Fatalf("apps = %v, want chrome and zoom only, topmost first", names)
	}
	chrome := got[0]
	if chrome.app.Title != "Lecture - Chrome" || chrome.app.PID != 200 || chrome.root.pid != 200 {
		t.Fatalf("chrome = %+v, want the topmost title and the main process", chrome.app)
	}
	if got[1].app.PID != 300 {
		t.Fatalf("zoom = %+v", got[1].app)
	}
}

func TestAppsOfFiltersWindows(t *testing.T) {
	base := func(w windowInfo, p procInfo) snapshot {
		return snapshot{self: ourPID, selfSession: ourSession, procs: []procInfo{proc(ourPID, 1, "la.exe", 5), p}, windows: []windowInfo{w}}
	}
	zoom := proc(300, 1, "zoom.exe", 300)
	tests := []struct {
		name string
		snap snapshot
		want []string
	}{
		{"listed", base(win(300, "Zoom"), zoom), []string{"zoom.exe"}},
		{"invisible", base(windowInfo{owner: 300, title: "Zoom"}, zoom), nil},
		{"child window", base(windowInfo{owner: 300, visible: true, style: wsChild}, zoom), nil},
		{"tool window", base(windowInfo{owner: 300, visible: true, exStyle: wsExToolWindow}, zoom), nil},
		{"no owner", base(win(0, "x"), zoom), nil},
		{"owner not in the snapshot", base(win(301, "x"), zoom), nil},
		{"another session", base(win(300, "Zoom"), procInfo{pid: 300, exe: "zoom.exe", created: 300, session: 2, sessionOK: true}), nil},
		{"unknown session", base(win(300, "Zoom"), procInfo{pid: 300, exe: "zoom.exe", created: 300}), nil},
		{"shell host", base(win(300, "Start"), proc(300, 1, "ShellExperienceHost.exe", 300)), nil},
		{"uwp frame without its app", base(win(300, "Calculator"), proc(300, 1, "ApplicationFrameHost.exe", 300)), nil},
		{"windows internal", base(win(300, "x"), proc(300, 1, "windowsinternal.composableshell.exe", 300)), nil},
		{"explorer without a title", base(win(300, ""), proc(300, 1, "explorer.exe", 300)), nil},
		{"explorer folder window", base(win(300, "Downloads"), proc(300, 1, "explorer.exe", 300)), []string{"explorer.exe"}},
		{"no executable name", base(win(300, "x"), proc(300, 1, "System", 300)), nil},
		{"own window", base(win(ourPID, "paratech"), zoom), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appNames(appsOf(tt.snap, maxApps)); !slices.Equal(got, tt.want) {
				t.Fatalf("apps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppsOfLimit(t *testing.T) {
	s := snapshot{self: ourPID, selfSession: ourSession}
	for i := range uint32(10) {
		pid := 1000 + i
		s.procs = append(s.procs, proc(pid, 1, "app"+string(rune('a'+i))+".exe", uint64(pid)))
		s.windows = append(s.windows, win(pid, "w"))
	}
	if got := appsOf(s, 3); len(got) != 3 || got[0].app.App != "appa.exe" {
		t.Fatalf("apps = %v, want the first 3", appNames(got))
	}
}

func TestTreeGuardsReusedParentPIDs(t *testing.T) {
	// 201 claims 200 as its parent, but 200 was created after it: the real parent exited and its PID was reused.
	tr := newTree([]procInfo{proc(200, 10, "chrome.exe", 500), proc(201, 200, "chrome.exe", 300), proc(202, 201, "chrome.exe", 0)})
	if _, ok := tr.parentOf(tr.byPID[201]); ok {
		t.Fatal("a younger process was taken for the parent")
	}
	if _, ok := tr.parentOf(tr.byPID[202]); ok {
		t.Fatal("an unknown creation time linked a parent")
	}
	if root := tr.sameExeRoot(tr.byPID[201]); root.pid != 201 {
		t.Fatalf("root = %d, want 201", root.pid)
	}
	if sub := tr.subtree(200); len(sub) != 1 {
		t.Fatalf("subtree of 200 = %v, want only itself", sub)
	}
}

func TestTreeSurvivesACycle(t *testing.T) {
	// Equal creation times may link two processes to each other; the walks stay bounded.
	tr := newTree([]procInfo{proc(1, 2, "a.exe", 5), proc(2, 1, "a.exe", 5)})
	_ = tr.sameExeRoot(tr.byPID[1])
	if _, ok := tr.ancestorIn(1, map[uint32]int{99: 0}); ok {
		t.Fatal("found an ancestor in a cycle")
	}
	if sub := tr.subtree(1); len(sub) != 2 {
		t.Fatalf("subtree = %v", sub)
	}
}

func TestMarkSessions(t *testing.T) {
	tests := []struct {
		name           string
		sessions       []sessionInfo
		playing, muted []string
	}{
		{"chrome plays through its audio service", []sessionInfo{{pid: 202, state: sessionActive}}, []string{"chrome.exe"}, nil},
		{"inactive session", []sessionInfo{{pid: 300, state: 0}}, nil, nil},
		{"expired session", []sessionInfo{{pid: 300, state: sessionExpired, muted: true}}, nil, nil},
		{"system sounds", []sessionInfo{{pid: 300, state: sessionActive, system: true}}, nil, nil},
		{"no process", []sessionInfo{{pid: 0, state: sessionActive}}, nil, nil},
		{"muted in the mixer", []sessionInfo{{pid: 300, state: sessionActive, muted: true}}, []string{"zoom.exe"}, []string{"zoom.exe"}},
		{"volume 0 in the mixer", []sessionInfo{{pid: 300, volume: 0, volumeOK: true}}, nil, []string{"zoom.exe"}},
		{"volume unknown", []sessionInfo{{pid: 300, state: sessionActive}}, []string{"zoom.exe"}, nil},
		// Our WebView2 is a descendant of explorer through this app: explorer gets only its own sessions.
		{"our own sound", []sessionInfo{{pid: 101, state: sessionActive}}, nil, nil},
		{"unknown process", []sessionInfo{{pid: 999, state: sessionActive}}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := desktop()
			s.windows = append(s.windows, win(10, "Downloads")) // explorer is listed too
			s.sessions = tt.sessions
			var playing, muted []string
			for _, c := range appsOf(s, maxApps) {
				if c.app.Playing {
					playing = append(playing, c.app.App.String())
				}
				if c.app.Muted {
					muted = append(muted, c.app.App.String())
				}
			}
			if !slices.Equal(playing, tt.playing) || !slices.Equal(muted, tt.muted) {
				t.Fatalf("playing %v, muted %v; want %v, %v", playing, muted, tt.playing, tt.muted)
			}
		})
	}
}

func TestCleanTitle(t *testing.T) {
	long := strings.Repeat("я", maxTitleBytes) // two bytes each
	tests := []struct{ in, want string }{
		{"  Lecture  ", "Lecture"},
		{"a\nb\tc\x00d", "a b c d"},
		{"bad \xff byte", "bad   byte"},
		{"Zoom\u202egnp.exe", "Zoomgnp.exe"}, // a right-to-left override would show it reversed
		{"Lec\u200bture\u2066 \ufeff", "Lecture"},
		{"\u00adMeet", "Meet"},
		{long, strings.Repeat("я", maxTitleBytes/2)},
		{"x" + long, "x" + strings.Repeat("я", (maxTitleBytes-1)/2)},
	}
	for _, tt := range tests {
		if got := cleanTitle(tt.in); got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestImageApp(t *testing.T) {
	tests := []struct {
		path string
		want shared.AppName
	}{
		{`C:\Program Files\Zoom\bin\Zoom.exe`, "zoom.exe"},
		{`\Device\HarddiskVolume3\Windows\explorer.exe`, "explorer.exe"},
		{"/usr/bin/chrome.exe", "chrome.exe"},
		{"Zoom.exe", "zoom.exe"},
		{`C:\Windows\System32\`, ""},
		{"", ""},
		{`C:\tools\script.bat`, ""},
	}
	for _, tt := range tests {
		if got := imageApp(tt.path); got != tt.want {
			t.Errorf("imageApp(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// fakePlatform is the raw state of a fake Windows.
type fakePlatform struct {
	mu         sync.Mutex
	snap       snapshot
	snapErr    error
	ids        map[uint32]procIdentity
	inspectErr error
	icons      map[string][]byte
	iconCalls  map[string]int
	block      chan struct{} // when set, snapshot waits for it
	panics     bool
	sessionsOn []bool // the sessions argument of every snapshot
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{
		snap: desktop(),
		ids: map[uint32]procIdentity{
			200: {alive: true, image: `C:\Chrome\chrome.exe`, session: ourSession, created: 200},
			300: {alive: true, image: `C:\Zoom\Zoom.exe`, session: ourSession, created: 300},
		},
		icons:     map[string][]byte{`C:\Chrome\chrome.exe`: []byte("chrome-png")},
		iconCalls: map[string]int{},
	}
}

func (f *fakePlatform) snapshot(sessions bool) (snapshot, error) {
	f.mu.Lock()
	block, panics := f.block, f.panics
	f.sessionsOn = append(f.sessionsOn, sessions)
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if panics {
		panic("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.snap
	if !sessions {
		s.sessions = nil
	}
	return s, f.snapErr
}

func (f *fakePlatform) inspect(pid uint32) (procIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inspectErr != nil {
		return procIdentity{}, f.inspectErr
	}
	return f.ids[pid], nil
}

func (f *fakePlatform) icon(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iconCalls[path]++
	return f.icons[path]
}

func (f *fakePlatform) set(change func(f *fakePlatform)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func TestAppsList(t *testing.T) {
	p := newFakePlatform()
	p.snap.sessions = []sessionInfo{{pid: 202, state: sessionActive}, {pid: 300, muted: true}}
	a := newApps(p, nil)

	for range 2 {
		got, err := a.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := []values.AudioApp{
			{App: "chrome.exe", Title: "Lecture - Chrome", PID: 200, Playing: true, IconPNG: []byte("chrome-png")},
			{App: "zoom.exe", Title: "Zoom Meeting", PID: 300, Muted: true},
		}
		if len(got) != len(want) {
			t.Fatalf("List = %+v", got)
		}
		for i := range want {
			g, w := got[i], want[i]
			if g.App != w.App || g.Title != w.Title || g.PID != w.PID || g.Playing != w.Playing || g.Muted != w.Muted ||
				string(g.IconPNG) != string(w.IconPNG) {
				t.Fatalf("app %d = %+v, want %+v", i, g, w)
			}
		}
	}
	if p.iconCalls[`C:\Chrome\chrome.exe`] != 1 || p.iconCalls[`C:\Zoom\Zoom.exe`] != 2 {
		t.Fatalf("icon reads = %v, want one per icon read and one per List for a missing icon", p.iconCalls)
	}
	if !slices.Equal(p.sessionsOn, []bool{true, true}) {
		t.Fatalf("snapshots = %v, want the sessions read", p.sessionsOn)
	}
}

func TestAppsListWithoutSessions(t *testing.T) {
	p := newFakePlatform()
	p.snap.sessionsErr = errors.New("E_ACCESSDENIED")
	got, err := newApps(p, nil).List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("List = %v, %v; want the apps without the sessions", got, err)
	}
}

func TestAppsListIconBudget(t *testing.T) {
	p := newFakePlatform()
	a := newApps(p, nil)
	clock := time.Unix(0, 0)
	a.now = func() time.Time { clock = clock.Add(iconBudget); return clock } // the budget is gone after the snapshot
	got, err := a.List(context.Background())
	if err != nil || got[0].IconPNG != nil || len(p.iconCalls) != 0 {
		t.Fatalf("List = %+v, %v, icon reads %v; want no icon read past the budget", got, err, p.iconCalls)
	}
	a.now = time.Now
	if got, _ := a.List(context.Background()); string(got[0].IconPNG) != "chrome-png" {
		t.Fatal("a later List does not read the icon")
	}
}

// An icon that could not be read is not cached: a later List reads it again.
func TestAppsListRetriesAMissingIcon(t *testing.T) {
	p := newFakePlatform()
	p.icons = map[string][]byte{}
	a := newApps(p, nil)
	if got, err := a.List(context.Background()); err != nil || got[0].IconPNG != nil {
		t.Fatalf("List = %+v, %v; want no icon yet", got, err)
	}
	p.set(func(f *fakePlatform) { f.icons[`C:\Chrome\chrome.exe`] = []byte("chrome-png") })
	if got, _ := a.List(context.Background()); string(got[0].IconPNG) != "chrome-png" {
		t.Fatal("a later List does not read the icon again")
	}
}

func TestAppsListIconFailures(t *testing.T) {
	p := newFakePlatform()
	p.inspectErr = errors.New("access denied")
	got, err := newApps(p, nil).List(context.Background())
	if err != nil || len(got) != 2 || got[0].IconPNG != nil {
		t.Fatalf("List = %+v, %v; an icon that cannot be read is nil", got, err)
	}
}

func TestAppsIconCacheIsBounded(t *testing.T) {
	p := newFakePlatform()
	a := newApps(p, nil)
	for i := range maxIconCache + 5 {
		path := `C:\app` + strings.Repeat("x", i) + ".exe"
		p.set(func(f *fakePlatform) { f.ids[7] = procIdentity{alive: true, image: path} })
		a.iconOf(7, true)
	}
	if len(a.icons) > maxIconCache {
		t.Fatalf("%d cached icons", len(a.icons))
	}
}

func TestAppsListFailures(t *testing.T) {
	t.Run("snapshot error", func(t *testing.T) {
		p := newFakePlatform()
		p.snapErr = errors.New("CreateToolhelp32Snapshot: denied")
		if _, err := newApps(p, nil).List(context.Background()); !errors.Is(err, ErrAppList) {
			t.Fatalf("List = %v, want ErrAppList", err)
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		_, err := newApps(unavailableAppsForTest{}, nil).List(context.Background())
		if !errors.Is(err, ErrAppList) || !errors.Is(err, recording.ErrCaptureUnavailable) {
			t.Fatalf("List = %v", err)
		}
	})
	t.Run("panic", func(t *testing.T) {
		p := newFakePlatform()
		p.panics = true
		if _, err := newApps(p, nil).List(context.Background()); !errors.Is(err, ErrAppList) {
			t.Fatalf("List = %v, want ErrAppList", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p := newFakePlatform()
		a := newApps(p, nil)
		for range 20 { // with a free gate a select alone would start the call about half the time
			if _, err := a.List(ctx); !errors.Is(err, ErrAppList) || !errors.Is(err, context.Canceled) {
				t.Fatalf("List = %v", err)
			}
		}
		var n int
		p.set(func(f *fakePlatform) { n = len(f.sessionsOn) })
		if n != 0 {
			t.Fatalf("%d snapshots for a cancelled context, want none", n)
		}
	})
}

// A platform call that hangs is given up after the timeout; the next call waits for it instead of piling up.
func TestAppsBoundsAHungPlatform(t *testing.T) {
	p := newFakePlatform()
	release := make(chan struct{})
	p.block = release
	a := newApps(p, nil)
	a.timeout = 50 * time.Millisecond

	if _, err := a.List(context.Background()); !errors.Is(err, ErrAppList) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("List = %v, want the timeout", err)
	}
	if _, err := a.List(context.Background()); !errors.Is(err, ErrAppList) {
		t.Fatalf("List while the hung call runs = %v, want the timeout", err)
	}
	var n int
	p.set(func(f *fakePlatform) { n = len(f.sessionsOn) })
	if n != 1 {
		t.Fatalf("%d snapshots, want the second call to wait for the first", n)
	}
	close(release)
	p.set(func(f *fakePlatform) { f.block = nil })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := a.List(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the apps never recovered from the hung call")
		}
	}
}

func TestAppsFind(t *testing.T) {
	zoom := ProcessIdentity{App: "zoom.exe", PID: 300, Created: 300}
	tests := []struct {
		name   string
		app    shared.AppName
		change func(f *fakePlatform)
		want   ProcessIdentity
		reason string // in the error; empty: found
	}{
		{"found", "zoom.exe", nil, zoom, ""},
		{"the root of a browser", "chrome.exe", nil, ProcessIdentity{App: "chrome.exe", PID: 200, Created: 200}, ""},
		{"not open", "teams.exe", nil, ProcessIdentity{}, "has no window"},
		{"invalid name", "Zoom", nil, ProcessIdentity{}, "invalid app name"},
		{"exited", "zoom.exe", func(f *fakePlatform) { f.ids[300] = procIdentity{} }, ProcessIdentity{}, "exited"},
		{"pid reused by another program", "zoom.exe", func(f *fakePlatform) {
			f.ids[300] = procIdentity{alive: true, image: `C:\Other\other.exe`, session: ourSession, created: 300}
		}, ProcessIdentity{}, "runs another program"},
		{"pid reused by the same program", "zoom.exe", func(f *fakePlatform) {
			f.ids[300] = procIdentity{alive: true, image: `C:\Zoom\Zoom.exe`, session: ourSession, created: 999}
		}, ProcessIdentity{}, "was replaced"},
		{"another session", "zoom.exe", func(f *fakePlatform) {
			f.ids[300] = procIdentity{alive: true, image: `C:\Zoom\Zoom.exe`, session: 2, created: 300}
		}, ProcessIdentity{}, "another Windows session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakePlatform()
			if tt.change != nil {
				tt.change(p)
			}
			got, err := newApps(p, nil).Find(context.Background(), tt.app)
			if tt.reason == "" {
				if err != nil || got != tt.want {
					t.Fatalf("Find = %+v, %v; want %+v", got, err, tt.want)
				}
				return
			}
			if !errors.Is(err, ErrAppNotRunning) || !errors.Is(err, recording.ErrSourceNotRunning) || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("Find = %+v, %v; want ErrAppNotRunning (%s)", got, err, tt.reason)
			}
		})
	}
	// A failure to look is no "app not running": the app may well run, so the popup says "failed", not "source_gone".
	failures := []struct {
		name   string
		change func(f *fakePlatform)
	}{
		{"list failure", func(f *fakePlatform) { f.snapErr = errors.New("denied") }},
		{"the process cannot be checked", func(f *fakePlatform) { f.inspectErr = errors.New("access denied") }},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakePlatform()
			tt.change(p)
			_, err := newApps(p, nil).Find(context.Background(), "zoom.exe")
			if !errors.Is(err, ErrAppList) || errors.Is(err, recording.ErrSourceNotRunning) {
				t.Fatalf("Find = %v, want ErrAppList", err)
			}
			if got := prompt.ProblemOf(err); got != prompt.ProblemFailed {
				t.Fatalf("problem = %q, want %q", got, prompt.ProblemFailed)
			}
		})
	}
}

func TestAppsSame(t *testing.T) {
	zoom := ProcessIdentity{App: "zoom.exe", PID: 300, Created: 300}
	tests := []struct {
		name string
		id   procIdentity
		same bool
	}{
		{"running", procIdentity{alive: true, image: `C:\Zoom\Zoom.exe`, session: ourSession, created: 300}, true},
		{"exited", procIdentity{created: 300}, false},
		{"unknown pid", procIdentity{}, false},
		{"pid reused by another program", procIdentity{alive: true, image: `C:\x\notepad.exe`, created: 300}, false},
		{"pid reused by a new instance", procIdentity{alive: true, image: `C:\Zoom\Zoom.exe`, created: 301}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakePlatform()
			p.ids[300] = tt.id
			same, err := newApps(p, nil).Same(context.Background(), zoom)
			if err != nil || same != tt.same {
				t.Fatalf("Same = %v, %v; want %v", same, err, tt.same)
			}
		})
	}
	p := newFakePlatform()
	p.inspectErr = errors.New("access denied")
	if _, err := newApps(p, nil).Same(context.Background(), zoom); !errors.Is(err, ErrAppList) {
		t.Fatalf("Same = %v, want ErrAppList", err)
	}
}

// unavailableAppsForTest is the platform of a build without capture, on every OS.
type unavailableAppsForTest struct{}

func (unavailableAppsForTest) snapshot(bool) (snapshot, error) { return snapshot{}, ErrUnavailable }
func (unavailableAppsForTest) inspect(uint32) (procIdentity, error) {
	return procIdentity{}, ErrUnavailable
}
func (unavailableAppsForTest) icon(string) []byte { return nil }
