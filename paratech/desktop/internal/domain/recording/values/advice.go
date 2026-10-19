package values

import (
	"time"

	"paratech/desktop/internal/domain/shared"
)

// The silent-source advice (TASK-082): during a recording of one app, the capture hears nothing while another app
// plays. The owner's case: a video played in Comet while Yandex Browser was chosen, and three recordings were zeros.
// SourceWatch is a pure state machine over observations; the recording session feeds it.

const (
	// SilentSourceAfter is how long the chosen app must stay silent while another app sounds before the advice shows
	// (the threshold of the healthy-but-mute detector, ADR-0002).
	SilentSourceAfter = 15 * time.Second

	// adviceGap is how long the other apps may seem silent (a pause of speech, a buffering video, a look that missed
	// the peak) and still count as sounding the whole time: up to one missed look of the session.
	adviceGap = 10 * time.Second
)

// SourceAdvice suggests another source for a recording whose app is silent. The names are untrusted text (plain text
// only). The zero value is no advice.
type SourceAdvice struct {
	Silent     shared.AppName // the chosen app, which gives no sound
	SilentName string
	// Suggest is the one other app that sounds, or the whole computer when several do.
	Suggest     Source
	SuggestName string // empty for the whole computer
}

// IsZero reports no advice.
func (a SourceAdvice) IsZero() bool { return a.Silent.IsZero() }

// SourceObservation is one look at a running recording.
type SourceObservation struct {
	At    time.Time
	Heard bool // the capture heard sound since the previous observation
	// Listed reports that Apps is the current list of apps; the session lists them only while the capture hears
	// nothing, and a failed listing tells nothing.
	Listed bool
	Apps   []AudioApp
}

// SourceWatch decides whether to advise another source. Create it with NewSourceWatch; it is a value: every method
// returns the next state.
type SourceWatch struct {
	source Source

	last       time.Time // the latest observation
	quietSince time.Time // first observation of the current silence; zero while the source is heard
	otherSince time.Time // since when other apps sound, short gaps included; zero when none
	otherLast  time.Time // the latest observation where another app sounded

	suggest     Source
	suggestName string
	silentName  string
	suggesting  bool // suggest is set

	dismissed    bool
	dismissedFor Source
}

// NewSourceWatch watches a recording of source. A whole-computer source never gets advice: it records everything.
func NewSourceWatch(source Source) SourceWatch { return SourceWatch{source: source} }

// Source returns the watched source.
func (w SourceWatch) Source() Source { return w.source }

// Observe records o. Sound heard ends the silence, and a dismissal with it (the next silence counts again).
func (w SourceWatch) Observe(o SourceObservation) SourceWatch {
	if w.source.IsWholeComputer() {
		return w
	}
	if o.At.Before(w.last) { // the wall clock went back: count from now
		w.quietSince, w.otherSince, w.otherLast = time.Time{}, time.Time{}, time.Time{}
	}
	w.last = o.At
	if o.Heard {
		w.quietSince, w.otherSince, w.otherLast = time.Time{}, time.Time{}, time.Time{}
		w.suggesting, w.dismissed = false, false
		return w
	}
	if w.quietSince.IsZero() {
		w.quietSince = o.At
	}
	if !o.Listed {
		return w
	}

	var others []AudioApp
	w.silentName = DisplayName(w.source.App(), "", "")
	for _, a := range o.Apps {
		switch {
		case a.App == w.source.App():
			if a.Name != "" {
				w.silentName = a.Name
			}
		case a.Sounding:
			others = append(others, a)
		}
	}
	if len(others) == 0 {
		if w.otherLast.IsZero() || o.At.Sub(w.otherLast) > adviceGap {
			w.otherSince, w.otherLast, w.suggesting = time.Time{}, time.Time{}, false
		}
		return w
	}

	if w.otherSince.IsZero() || o.At.Sub(w.otherLast) > adviceGap {
		w.otherSince = o.At
	}
	w.otherLast = o.At
	w.suggest, w.suggestName = WholeComputer(), ""
	if len(others) == 1 {
		if s, err := SourceOf(others[0].App); err == nil {
			w.suggest, w.suggestName = s, others[0].Name
			if w.suggestName == "" {
				w.suggestName = DisplayName(others[0].App, "", "")
			}
		}
	}
	w.suggesting = true
	if w.dismissed && w.suggest != w.dismissedFor {
		w.dismissed = false // a new suggestion is shown again
	}
	return w
}

// Advice returns the advice to show now, if any: the chosen app has been silent for SilentSourceAfter while other
// apps sounded the whole time, and the user did not dismiss this suggestion.
func (w SourceWatch) Advice() (SourceAdvice, bool) {
	if !w.suggesting || w.dismissed || w.quietSince.IsZero() || w.otherSince.IsZero() ||
		w.last.Sub(w.quietSince) < SilentSourceAfter || w.last.Sub(w.otherSince) < SilentSourceAfter {
		return SourceAdvice{}, false
	}
	return SourceAdvice{
		Silent: w.source.App(), SilentName: w.silentName, Suggest: w.suggest, SuggestName: w.suggestName,
	}, true
}

// Dismiss hides the current suggestion («Оставить») until it changes, or the source is heard and then silent again.
func (w SourceWatch) Dismiss() SourceWatch {
	if w.suggesting {
		w.dismissed, w.dismissedFor = true, w.suggest
	}
	return w
}
