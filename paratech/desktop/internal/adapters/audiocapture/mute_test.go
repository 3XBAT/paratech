package audiocapture

import (
	"testing"
	"time"
)

// muteStep is one input of the detector: time passes, then a packet with or without sound, or a meter reading.
type muteStep struct {
	after    time.Duration
	sound    bool // a packet with sound
	read     bool // a meter reading (when due)
	expected bool // what the meters show
}

// The detector state machine on synthetic input: (time, packet with sound?, meters show sound?).
func TestMuteDetector(t *testing.T) {
	reading := func(after time.Duration, expected bool) muteStep {
		return muteStep{after: after, read: true, expected: expected}
	}
	repeat := func(n int, s muteStep) []muteStep {
		out := make([]muteStep, n)
		for i := range out {
			out[i] = s
		}
		return out
	}
	join := func(parts ...[]muteStep) []muteStep {
		var out []muteStep
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	tests := []struct {
		name  string
		steps []muteStep
		want  int // the step at which the detector trips; -1 = never
	}{
		{"zeros while the app plays trip at the threshold", repeat(20, reading(time.Second, true)), 14},
		{"a muted app (the meters show nothing) never trips", repeat(120, reading(time.Second, false)), -1},
		{"sound resets the count", join(repeat(10, reading(time.Second, true)), []muteStep{{sound: true}},
			repeat(14, reading(time.Second, true)), repeat(2, reading(time.Second, true))), 25},
		{"pauses of the meter do not reset the count", join(repeat(7, reading(time.Second, true)),
			repeat(5, reading(time.Second, false)), repeat(10, reading(time.Second, true))), 19},
		{"a stalled loop counts two intervals at most", join([]muteStep{reading(time.Minute, true)},
			repeat(20, reading(time.Second, true))), 13},
		{"the clock going back counts nothing", join([]muteStep{reading(-time.Hour, true)},
			repeat(20, reading(time.Second, true))), 15},
		{"one reading per interval", repeat(200, muteStep{after: 100 * time.Millisecond, read: true, expected: true}), 149},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
			d := newMuteDetector(15*time.Second, time.Second, now)
			got := -1
			for i, s := range tt.steps {
				now = now.Add(s.after)
				switch {
				case s.sound:
					d.heard(now)
				case s.read && d.due(now):
					if d.sample(now, s.expected) && got < 0 {
						got = i
					}
				}
			}
			if got != tt.want {
				t.Fatalf("tripped at step %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSilentData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"nothing", nil, true},
		{"zeros", make([]byte, 64), true},
		{"one sample", append(make([]byte, 63), 1), false},
		{"float -0.0", []byte{0, 0, 0, 0x80}, false},
	}
	for _, tt := range tests {
		if got := silentData(tt.data); got != tt.want {
			t.Errorf("%s: silentData = %v", tt.name, got)
		}
	}
}

// Which sessions make the meters show sound on an INCLUDE target: audible ones of the target's tree only.
func TestAudibleInTree(t *testing.T) {
	// 100 chrome (root) -> 101 chrome audio service; 200 zoom; 300 explorer -> 301 notepad.
	procs := []procInfo{
		{pid: 100, parent: 300, exe: "chrome.exe", created: 10},
		{pid: 101, parent: 100, exe: "chrome.exe", created: 11},
		{pid: 200, parent: 300, exe: "Zoom.exe", created: 12},
		{pid: 300, parent: 4, exe: "explorer.exe", created: 5},
		{pid: 301, parent: 300, exe: "notepad.exe", created: 13},
	}
	playing := func(pid uint32) sessionInfo {
		return sessionInfo{pid: pid, state: sessionActive, peak: 0.3, peakOK: true, volume: 1, volumeOK: true}
	}
	with := func(s sessionInfo, change func(*sessionInfo)) sessionInfo { change(&s); return s }
	tests := []struct {
		name     string
		root     uint32
		sessions []sessionInfo
		want     bool
	}{
		{"the audio service of the browser plays", 100, []sessionInfo{playing(101)}, true},
		{"the root itself plays", 100, []sessionInfo{playing(100)}, true},
		{"another app plays", 100, []sessionInfo{playing(200)}, false},
		{"nothing plays", 100, nil, false},
		{"peak zero", 100, []sessionInfo{with(playing(101), func(s *sessionInfo) { s.peak = 0 })}, false},
		{"no peak meter", 100, []sessionInfo{with(playing(101), func(s *sessionInfo) { s.peakOK = false })}, false},
		{"muted in the mixer", 100, []sessionInfo{with(playing(101), func(s *sessionInfo) { s.muted = true })}, false},
		{"mixer slider at 0", 100, []sessionInfo{with(playing(101), func(s *sessionInfo) { s.volume = 0 })}, false},
		{"inactive session", 100, []sessionInfo{with(playing(101), func(s *sessionInfo) { s.state = 0 })}, false},
		{"system sounds", 300, []sessionInfo{with(playing(300), func(s *sessionInfo) { s.system = true })}, false},
		{"a process unknown to the snapshot", 100, []sessionInfo{playing(999)}, false},
		{"one of several", 200, []sessionInfo{playing(101), with(playing(200), func(s *sessionInfo) { s.muted = true }),
			playing(200)}, true},
		// INCLUDE of the shell captures what it started, so a child of explorer counts (Windows' tree, not the picker's).
		{"a child of the shell", 300, []sessionInfo{playing(301)}, true},
	}
	tr := newTree(procs)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audibleInTree(tr, tt.root, tt.sessions); got != tt.want {
				t.Fatalf("audibleInTree = %v, want %v", got, tt.want)
			}
			if tt.want && !anyAudible(tt.sessions) {
				t.Fatal("anyAudible must hold when a session of the tree is audible")
			}
		})
	}
}

func TestSourceConfigMuteThreshold(t *testing.T) {
	tests := []struct {
		in, want time.Duration
	}{
		{0, DefaultMuteThreshold},
		{-time.Second, 0},
		{30 * time.Second, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := (SourceConfig{MuteThreshold: tt.in}).muteAfter(); got != tt.want {
			t.Errorf("MuteThreshold %v: %v, want %v", tt.in, got, tt.want)
		}
	}
	if DefaultSourceConfig().muteAfter() != 15*time.Second {
		t.Fatal("the default threshold is the 15 s of ADR-0002")
	}
}
