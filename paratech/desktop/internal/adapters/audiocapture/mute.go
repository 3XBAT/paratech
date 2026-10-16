package audiocapture

import "time"

// The "healthy but mute" detector (ADR-0002 Decision 3, MeetCap #38): a process loopback stream that runs and delivers
// packets but only zeros although the target plays sound. It counts the time during which audio was expected (the
// Windows meters show sound on the target) and no packet had any; past the threshold the stream falls back to classic
// loopback. It is a pure state machine over (time, packet with sound, meter reading), so it is tested on every OS;
// the Windows meters are read in wasapi_meter_windows.go.
//
// Audio is expected only when a meter shows sound. A muted tab (Chromium closes its stream: no active session), an
// app that renders silence, or a session muted or at volume 0 in the volume mixer shows none, so they never count:
// classic loopback would be just as silent there, and switching would only lose the app filter.

const (
	// DefaultMuteThreshold is how long a process loopback stream may deliver only zeros while audio is expected before
	// it falls back (ADR-0002 Decision 3: a proposal, to be tuned with real devices).
	DefaultMuteThreshold = 15 * time.Second

	// meterInterval is how often the meters are read while the capture hears nothing.
	meterInterval = time.Second
)

// muteDetector is the state of the detector. The zero value is not usable; see newMuteDetector.
type muteDetector struct {
	threshold time.Duration
	every     time.Duration

	quiet time.Duration // expected time without any packet with sound
	mark  time.Time     // since when the next reading counts: the last reading or the last packet with sound
}

func newMuteDetector(threshold, every time.Duration, now time.Time) *muteDetector {
	return &muteDetector{threshold: threshold, every: every, mark: now}
}

// heard records a packet with sound at now: the stream is not mute, the count starts again.
func (d *muteDetector) heard(now time.Time) {
	d.quiet, d.mark = 0, now
}

// due reports whether a meter reading is wanted at now: one per interval while nothing is heard.
func (d *muteDetector) due(now time.Time) bool {
	if now.Before(d.mark) {
		d.mark = now // the wall clock went back: count from now
	}
	return now.Sub(d.mark) >= d.every
}

// sample records a meter reading at now and reports whether the stream is mute for longer than the threshold. The
// time since the previous reading counts only when expected; a stalled loop counts at most two intervals.
func (d *muteDetector) sample(now time.Time, expected bool) (mute bool) {
	if expected {
		d.quiet += min(max(now.Sub(d.mark), 0), 2*d.every)
	}
	d.mark = now
	return d.quiet >= d.threshold
}

// audible reports a session that plays sound now by the meters: active, not the system sounds, a peak above zero,
// and neither muted nor at volume 0 in the volume mixer (whether the session meter reads before or after the session
// volume is not confirmed, so both are checked).
func (s sessionInfo) audible() bool {
	return s.state == sessionActive && !s.system && s.pid != 0 && s.peakOK && s.peak > 0 && !s.muted &&
		!(s.volumeOK && s.volume <= 0)
}

// anyAudible reports whether any session is audible.
func anyAudible(sessions []sessionInfo) bool {
	for _, s := range sessions {
		if s.audible() {
			return true
		}
	}
	return false
}

// audibleInTree reports an audible session whose process is root or a descendant of it: the tree process loopback
// INCLUDE captures. Session PIDs are walked up because Chromium plays through a child process.
func audibleInTree(t tree, root uint32, sessions []sessionInfo) bool {
	roots := map[uint32]int{root: 0}
	for _, s := range sessions {
		if !s.audible() {
			continue
		}
		if _, ok := t.ancestorIn(s.pid, roots); ok {
			return true
		}
	}
	return false
}

// silentData reports whether every byte of a packet is zero: the "only zeros" of a mute process loopback stream. An
// IEEE -0.0 sample (sign bit set) counts as sound, which errs on the side of not switching.
func silentData(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
