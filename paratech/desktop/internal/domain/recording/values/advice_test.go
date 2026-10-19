package values

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

// owner case: Yandex Browser chosen, the video plays in Comet.
func ownerApps() []AudioApp {
	return []AudioApp{
		{App: "browser.exe", Name: "Яндекс Браузер"},
		{App: "comet.exe", Name: "Comet", Sounding: true, Playing: true},
		{App: "zoom.exe", Name: "Zoom"},
	}
}

func silentLook(at time.Duration, apps []AudioApp) SourceObservation {
	return SourceObservation{At: t0.Add(at), Listed: true, Apps: apps}
}

// feed observes every 5 s from from to to (inclusive) and returns the watch.
func feed(w SourceWatch, from, to time.Duration, look func(time.Duration) SourceObservation) SourceWatch {
	for at := from; at <= to; at += 5 * time.Second {
		w = w.Observe(look(at))
	}
	return w
}

func browser(t *testing.T) Source {
	t.Helper()
	s, err := SourceOf("browser.exe")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSourceWatchAdvisesAfterThreshold(t *testing.T) {
	look := func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) }
	w := feed(NewSourceWatch(browser(t)), 0, 10*time.Second, look)
	if a, ok := w.Advice(); ok {
		t.Fatalf("advice after 10 s: %+v", a)
	}

	w = w.Observe(look(15 * time.Second))
	a, ok := w.Advice()
	if !ok {
		t.Fatal("no advice after 15 s")
	}
	want := SourceAdvice{Silent: "browser.exe", SilentName: "Яндекс Браузер", Suggest: Source{app: "comet.exe"}, SuggestName: "Comet"}
	if a != want {
		t.Fatalf("advice %+v, want %+v", a, want)
	}
}

func TestSourceWatchNoAdvice(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		look   func(time.Duration) SourceObservation
	}{
		{"the chosen app is heard", Source{app: "browser.exe"}, func(at time.Duration) SourceObservation {
			return SourceObservation{At: t0.Add(at), Heard: true}
		}},
		{"nothing else sounds", Source{app: "browser.exe"}, func(at time.Duration) SourceObservation {
			return silentLook(at, []AudioApp{{App: "browser.exe"}, {App: "comet.exe", Playing: true}})
		}},
		{"whole computer", WholeComputer(), func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) }},
		{"apps never listed", Source{app: "browser.exe"}, func(at time.Duration) SourceObservation {
			return SourceObservation{At: t0.Add(at)}
		}},
		{"only the chosen app sounds", Source{app: "browser.exe"}, func(at time.Duration) SourceObservation {
			return silentLook(at, []AudioApp{{App: "browser.exe", Sounding: true}})
		}},
	}
	for _, tt := range tests {
		w := feed(NewSourceWatch(tt.source), 0, time.Minute, tt.look)
		if a, ok := w.Advice(); ok {
			t.Errorf("%s: advice %+v", tt.name, a)
		}
	}
}

func TestSourceWatchSoundResets(t *testing.T) {
	look := func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) }
	w := feed(NewSourceWatch(browser(t)), 0, 10*time.Second, look)
	w = w.Observe(SourceObservation{At: t0.Add(12 * time.Second), Heard: true})
	w = feed(w, 15*time.Second, 25*time.Second, look)
	if _, ok := w.Advice(); ok {
		t.Fatal("advice although the chosen app was heard 13 s ago")
	}
	w = w.Observe(look(30 * time.Second))
	if _, ok := w.Advice(); !ok {
		t.Fatal("no advice after 15 s of new silence")
	}
}

func TestSourceWatchGaps(t *testing.T) {
	// The other app pauses for one look (under the gap): still advised.
	w := NewSourceWatch(browser(t))
	for at := time.Duration(0); at <= 15*time.Second; at += 3 * time.Second {
		apps := ownerApps()
		if at == 6*time.Second {
			apps[1].Sounding = false
		}
		w = w.Observe(silentLook(at, apps))
	}
	if _, ok := w.Advice(); !ok {
		t.Fatal("a short pause of the other app must not reset the count")
	}

	// A pause of the other app longer than the gap starts its count again.
	w = NewSourceWatch(browser(t))
	quietComet := ownerApps()
	quietComet[1].Sounding = false
	w = w.Observe(silentLook(0, ownerApps()))
	w = w.Observe(silentLook(5*time.Second, quietComet))
	w = w.Observe(silentLook(10*time.Second, quietComet))
	w = feed(w, 15*time.Second, 20*time.Second, func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) })
	if _, ok := w.Advice(); ok {
		t.Fatal("the other app sounded only for 5 s")
	}
	w = feed(w, 25*time.Second, 30*time.Second, func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) })
	if _, ok := w.Advice(); !ok {
		t.Fatal("no advice after the other app sounded 15 s")
	}
}

func TestSourceWatchSeveralOthersSuggestWholeComputer(t *testing.T) {
	apps := ownerApps()
	apps[2].Sounding = true
	w := feed(NewSourceWatch(browser(t)), 0, 15*time.Second, func(at time.Duration) SourceObservation { return silentLook(at, apps) })
	a, ok := w.Advice()
	if !ok || !a.Suggest.IsWholeComputer() || a.SuggestName != "" {
		t.Fatalf("advice %+v, %v; want the whole computer", a, ok)
	}
}

func TestSourceWatchNotRunningApp(t *testing.T) {
	// The chosen app is closed: its name comes from the known list.
	apps := []AudioApp{{App: "comet.exe", Name: "Comet", Sounding: true}}
	w := feed(NewSourceWatch(browser(t)), 0, 15*time.Second, func(at time.Duration) SourceObservation { return silentLook(at, apps) })
	a, ok := w.Advice()
	if !ok || a.SilentName != "Яндекс Браузер" {
		t.Fatalf("advice %+v, %v", a, ok)
	}
}

func TestSourceWatchDismiss(t *testing.T) {
	look := func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) }
	w := feed(NewSourceWatch(browser(t)), 0, 15*time.Second, look).Dismiss()
	w = feed(w, 20*time.Second, time.Minute, look)
	if a, ok := w.Advice(); ok {
		t.Fatalf("dismissed advice shown again: %+v", a)
	}

	// The suggestion changes: shown again.
	zoomToo := ownerApps()
	zoomToo[2].Sounding = true
	w = w.Observe(silentLook(65*time.Second, zoomToo))
	if a, ok := w.Advice(); !ok || !a.Suggest.IsWholeComputer() {
		t.Fatalf("a new suggestion must show: %+v, %v", a, ok)
	}

	// Heard, then silent again: shown again after the threshold.
	w = feed(NewSourceWatch(browser(t)), 0, 15*time.Second, look).Dismiss()
	w = w.Observe(SourceObservation{At: t0.Add(20 * time.Second), Heard: true})
	w = feed(w, 25*time.Second, 35*time.Second, look)
	if _, ok := w.Advice(); ok {
		t.Fatal("advice before 15 s of new silence")
	}
	w = w.Observe(look(40 * time.Second))
	if _, ok := w.Advice(); !ok {
		t.Fatal("the new silence must show the advice again")
	}

	// Dismissing without a suggestion does nothing.
	if w := NewSourceWatch(browser(t)).Dismiss(); w.dismissed {
		t.Fatal("dismissed without a suggestion")
	}
}

func TestSourceWatchClockBack(t *testing.T) {
	look := func(at time.Duration) SourceObservation { return silentLook(at, ownerApps()) }
	w := feed(NewSourceWatch(browser(t)), time.Minute, time.Minute+10*time.Second, look)
	w = w.Observe(look(5 * time.Second)) // back by more than a minute
	w = w.Observe(look(10 * time.Second))
	if _, ok := w.Advice(); ok {
		t.Fatal("the clock went back: the count must start again")
	}
	if got := w.Source().App(); got != shared.AppName("browser.exe") {
		t.Fatalf("source %s", got)
	}
}
