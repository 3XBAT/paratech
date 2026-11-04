package agentcompact

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// codes are the errors of the package; a failure matches exactly one.
var codes = []error{ErrInvalidConfig, ErrInvalidInput, ErrBusy, ErrChanged, ErrQuota, ErrUnavailable, ErrRefused, ErrStore}

func onlyCode(t *testing.T, err, want error) {
	t.Helper()
	for _, c := range codes {
		if errors.Is(err, c) != (c == want) {
			t.Fatalf("error %v: matches %v = %v, want only %v", err, c, errors.Is(err, c), want)
		}
	}
}

func TestNewNeedsEveryPort(t *testing.T) {
	ok := Deps{Chats: &memChats{}, History: &memHistory{}, Summarizer: &fakeHub{}, Clock: &fixedClock{}}
	if _, err := New(ok); err != nil {
		t.Fatalf("New = %v", err)
	}
	for name, edit := range map[string]func(*Deps){
		"chats": func(d *Deps) { d.Chats = nil }, "history": func(d *Deps) { d.History = nil },
		"summarizer": func(d *Deps) { d.Summarizer = nil }, "clock": func(d *Deps) { d.Clock = nil },
	} {
		d := ok
		edit(&d)
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

// A chat whose window has room needs no summary: no Hub call, nothing saved, the fill returned.
func TestNoCompactionNeeded(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 12, false)
	saves := e.chats.saved()

	got, err := e.svc.Compact(t.Context(), "c1")
	if err != nil || got != (Result{Fill: Fill{Percent: 60, Level: thread.FillNormal}}) {
		t.Fatalf("Compact = %+v, %v", got, err)
	}
	if len(e.hub.calls()) != 0 || e.chats.saved() != saves {
		t.Fatal("a chat with room called the Hub or saved")
	}
}

// A full chat is compacted with exactly one Hub call: the oldest ten requests, the key of their last; the summary is
// stored and the fill drops.
func TestCompactsAFullChat(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 21, false)

	got, err := e.svc.Compact(t.Context(), "c1")
	if err != nil || got != (Result{Compacted: true, Fill: Fill{Percent: 55, Level: thread.FillNormal}}) {
		t.Fatalf("Compact = %+v, %v", got, err)
	}
	calls := e.hub.calls()
	if len(calls) != 1 {
		t.Fatalf("%d Hub calls", len(calls))
	}
	var want []values.SummaryPair
	for n := 1; n <= 10; n++ {
		want = append(want, values.SummaryPair{Student: numbered(n), Reply: fmt.Sprintf("reply: ответ %d", n)})
	}
	if r := calls[0]; r.Key() != "cs:c1:10" || r.Through() != 10 || r.Previous() != "" || !reflect.DeepEqual(r.Pairs(), want) {
		t.Fatalf("request %s through %d previous %q pairs %+v", r.Key(), r.Through(), r.Previous(), r.Pairs())
	}
	if s := e.chats.stored(t, "c1").Summary(); s.Through() != 10 || s.Text() != "Сводка: обсуждали физику." {
		t.Fatalf("stored summary %+v", s)
	}

	// The next compaction carries the stored summary as the previous one.
	e.chat(t, "c2", 31, false)
	e.hub.answer = answering("Вторая сводка.")
	if _, err := e.svc.Compact(t.Context(), "c2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Compact(t.Context(), "c2"); err != nil {
		t.Fatal(err)
	}
	calls = e.hub.calls()
	if len(calls) != 3 || calls[2].Key() != "cs:c2:20" || calls[2].Previous() != "Вторая сводка." {
		t.Fatalf("second compaction of c2: %d calls, last %s previous %q", len(calls), calls[len(calls)-1].Key(), calls[len(calls)-1].Previous())
	}
}

// A failed Hub call is returned as its code; nothing is stored.
func TestHubFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"quota", fmt.Errorf("hub: %w", shared.ErrQuotaExceeded), ErrQuota},
		{"network", fmt.Errorf("hub: %w", shared.ErrNetworkUnavailable), ErrUnavailable},
		{"hub unavailable", errors.Join(errors.New("hub unavailable"), shared.ErrServiceUnavailable, backoff.ErrTransient), ErrUnavailable},
		{"not configured", errors.Join(errors.New("not configured"), shared.ErrServiceNotConfigured, backoff.ErrTransient), ErrUnavailable},
		{"timeout", errors.Join(errors.New("timeout"), backoff.ErrTransient), ErrUnavailable},
		{"rejected", errors.Join(errors.New("rejected"), backoff.ErrPermanent), ErrRefused},
		{"refused answer", errors.Join(errors.New("provider"), backoff.ErrPermanent, values.ErrSummaryRefused), ErrRefused},
		{"context", context.DeadlineExceeded, ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", 21, false)
			saves := e.chats.saved()
			e.hub.answer = func(int) (string, error) { return "", tt.err }

			got, err := e.svc.Compact(t.Context(), "c1")
			onlyCode(t, err, tt.want)
			if !errors.Is(err, tt.err) || got != (Result{}) {
				t.Fatalf("Compact = %+v, %v; the cause is lost", got, err)
			}
			if e.chats.saved() != saves || !e.chats.stored(t, "c1").Summary().IsZero() {
				t.Fatal("a failed compaction stored something")
			}
		})
	}
}

// The caller going away during the call is unavailable whatever the Hub says.
func TestCallerGone(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 21, false)
	ctx, cancel := context.WithCancel(t.Context())
	e.hub.answer = func(int) (string, error) {
		cancel()
		return "", errors.Join(errors.New("rejected"), backoff.ErrPermanent)
	}
	_, err := e.svc.Compact(ctx, "c1")
	onlyCode(t, err, ErrUnavailable)
}

// A text the chat may not keep is a refused summary: nothing is stored, and the key is not asked again.
func TestInvalidSummaryText(t *testing.T) {
	tests := map[string]string{
		"empty":     "  \n ",
		"control":   "сводка\x07",
		"bidi":      "сводка" + string(rune(0x202E)) + "txt",
		"too long":  strings.Repeat("я", values.MaxSummaryRunes+1),
		"too large": strings.Repeat("𝔸", values.MaxSummaryBytes/4+1),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", 21, false)
			saves := e.chats.saved()
			e.hub.answer = answering(text)

			_, err := e.svc.Compact(t.Context(), "c1")
			onlyCode(t, err, ErrRefused)
			if !errors.Is(err, values.ErrInvalidSummary) || e.chats.saved() != saves {
				t.Fatalf("Compact = %v, saves %d → %d", err, saves, e.chats.saved())
			}
			_, err = e.svc.Compact(t.Context(), "c1")
			onlyCode(t, err, ErrRefused)
			if len(e.hub.calls()) != 1 {
				t.Fatalf("the refused key was asked %d times", len(e.hub.calls()))
			}
		})
	}
}

// A refused answer is remembered by key (the Hub replays it, charged once); a network failure is not.
func TestRefusedKeyIsRemembered(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 21, false)
	e.hub.answer = func(int) (string, error) {
		return "", errors.Join(errors.New("provider"), backoff.ErrPermanent, values.ErrSummaryRefused)
	}
	for range 3 {
		_, err := e.svc.Compact(t.Context(), "c1")
		onlyCode(t, err, ErrRefused)
	}
	if n := len(e.hub.calls()); n != 1 {
		t.Fatalf("a refused key reached the Hub %d times", n)
	}

	// Another chat is another key.
	e.chat(t, "c2", 21, false)
	if _, err := e.svc.Compact(t.Context(), "c2"); !errors.Is(err, ErrRefused) || len(e.hub.calls()) != 2 {
		t.Fatalf("another key: %v, %d calls", err, len(e.hub.calls()))
	}

	// A permanent failure that is not a refused answer (a rejected key) is tried again.
	e.chat(t, "c3", 21, false)
	e.hub.answer = func(int) (string, error) { return "", errors.Join(errors.New("bad key"), backoff.ErrPermanent) }
	for range 2 {
		_, err := e.svc.Compact(t.Context(), "c3")
		onlyCode(t, err, ErrRefused)
	}
	if n := len(e.hub.calls()); n != 4 {
		t.Fatalf("a rejected request was not tried again: %d calls", n)
	}

	e.chat(t, "c4", 21, false)
	e.hub.answer = func(n int) (string, error) {
		if n == 5 {
			return "", fmt.Errorf("hub: %w", shared.ErrNetworkUnavailable)
		}
		return "Сводка.", nil
	}
	if _, err := e.svc.Compact(t.Context(), "c4"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("network = %v", err)
	}
	if got, err := e.svc.Compact(t.Context(), "c4"); err != nil || !got.Compacted || len(e.hub.calls()) != 6 {
		t.Fatalf("after the network came back: %+v, %v, %d calls", got, err, len(e.hub.calls()))
	}
}

func TestRefusedKeysAreBounded(t *testing.T) {
	e := newEnv(t)
	for i := range maxRefused + 1 {
		e.svc.remember(fmt.Sprintf("cs:c%d:10", i))
	}
	if len(e.svc.refused) != maxRefused || e.svc.wasRefused("cs:c0:10") || !e.svc.wasRefused(fmt.Sprintf("cs:c%d:10", maxRefused)) {
		t.Fatalf("%d keys remembered, the oldest kept %v", len(e.svc.refused), e.svc.wasRefused("cs:c0:10"))
	}
	e.svc.remember("cs:c1:10")
	if len(e.svc.refused) != maxRefused {
		t.Fatal("a key already remembered was added again")
	}
}

// once runs f on the first call only, so a competing save inside beforeSave does not run it again.
func once(f func()) func() {
	done := false
	return func() {
		if !done {
			done = true
			f()
		}
	}
}

// A chat saved while the summary was made by a change that leaves the request to summarize as it was (a rename, a
// save of a step) is reloaded and the same summary applied: no second Hub call.
func TestVersionConflictReappliesTheSummary(t *testing.T) {
	tests := map[string]func(t *testing.T, e *env){
		"bump": func(_ *testing.T, e *env) { e.chats.bump("c1") },
		"rename": func(t *testing.T, e *env) {
			th := e.chats.stored(t, "c1")
			if err := th.Rename("Физика", e.clock.Now()); err != nil {
				t.Error(err)
			}
			if err := e.chats.Save(t.Context(), th); err != nil {
				t.Error(err)
			}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", 21, false)
			e.chats.beforeSave = once(func() { change(t, e) })
			got, err := e.svc.Compact(t.Context(), "c1")
			if err != nil || got != (Result{Compacted: true, Fill: Fill{Percent: 55, Level: thread.FillNormal}}) {
				t.Fatalf("Compact = %+v, %v", got, err)
			}
			if len(e.hub.calls()) != 1 || e.chats.stored(t, "c1").Summary().Through() != 10 {
				t.Fatalf("%d Hub calls, stored %+v", len(e.hub.calls()), e.chats.stored(t, "c1").Summary())
			}
			if name == "rename" && e.chats.stored(t, "c1").Title() != "Физика" {
				t.Fatal("the rename was lost")
			}
		})
	}
}

// «Скрыть» (a restart from request n of those summarized) during the Hub call forgets requests the summary covers:
// the rebuilt request differs, so the summary is not stored and the Hub is not asked again.
func TestRestartDuringCallDropsTheSummary(t *testing.T) {
	for _, tt := range []struct {
		name string
		from int
		done bool
	}{
		{"running restart", 5, false},
		{"finished restart", 5, true},
		{"restart from the first", 1, true},
		{"restart from the last summarized", 10, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", 21, false)
			e.chats.beforeSave = once(func() {
				th := e.chats.stored(t, "c1")
				if err := th.RestartFrom(tt.from, thread.Request{Text: "заново"}, e.clock.Now()); err != nil {
					t.Error(err)
				}
				if tt.done {
					if err := th.Complete(e.clock.Now()); err != nil {
						t.Error(err)
					}
				}
				if err := e.chats.Save(t.Context(), th); err != nil {
					t.Error(err)
				}
				e.history.drop("c1", tt.from, 22)
			})
			got, err := e.svc.Compact(t.Context(), "c1")
			onlyCode(t, err, ErrChanged)
			if !errors.Is(err, errRequestChanged) || got.Compacted || len(e.hub.calls()) != 1 {
				t.Fatalf("Compact = %+v, %v, %d Hub calls", got, err, len(e.hub.calls()))
			}
			if !e.chats.stored(t, "c1").Summary().IsZero() {
				t.Fatalf("a summary of forgotten requests was stored: %+v", e.chats.stored(t, "c1").Summary())
			}
		})
	}
}

// A failed read or save of the store after the Hub answered is ErrStore with its cause (ErrUnavailable when the
// caller went away); nothing is stored and the Hub is asked once.
func TestStoreFailuresAfterTheAnswer(t *testing.T) {
	disk := errors.New("disk")
	tests := []struct {
		name  string
		setup func(e *env, cancel context.CancelFunc)
		want  error
		cause error
	}{
		{"save fails", func(e *env, _ context.CancelFunc) { e.chats.saveErr = disk }, ErrStore, disk},
		{"history read on reload fails", func(e *env, _ context.CancelFunc) {
			e.chats.beforeSave = once(func() { e.chats.bump("c1"); e.history.fail(disk) })
		}, ErrStore, disk},
		{"chat gone on reload", func(e *env, _ context.CancelFunc) {
			e.chats.beforeSave = once(func() { e.chats.remove("c1") })
		}, ErrStore, shared.ErrNotFound},
		{"caller gone after the answer", func(e *env, cancel context.CancelFunc) {
			e.hub.answer = func(int) (string, error) { cancel(); return "Сводка.", nil }
		}, ErrUnavailable, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", 21, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tt.setup(e, cancel)

			got, err := e.svc.Compact(ctx, "c1")
			onlyCode(t, err, tt.want)
			if !errors.Is(err, tt.cause) || got != (Result{}) || len(e.hub.calls()) != 1 {
				t.Fatalf("Compact = %+v, %v, %d Hub calls", got, err, len(e.hub.calls()))
			}
			e.chats.saveErr = nil
			if th, err := e.chats.Get(t.Context(), "c1"); err == nil && !th.Summary().IsZero() {
				t.Fatal("a failed compaction stored a summary")
			}
		})
	}
}

// A chat that keeps changing, or that took another summary meanwhile, does not take this one: ErrChanged, no second
// Hub call, the stored chat as the other change left it.
func TestVersionConflictGivesUp(t *testing.T) {
	t.Run("keeps changing", func(t *testing.T) {
		e := newEnv(t)
		e.chat(t, "c1", 21, false)
		e.chats.beforeSave = func() { e.chats.bump("c1") }
		_, err := e.svc.Compact(t.Context(), "c1")
		onlyCode(t, err, ErrChanged)
		if !errors.Is(err, shared.ErrVersionConflict) || len(e.hub.calls()) != 1 || !e.chats.stored(t, "c1").Summary().IsZero() {
			t.Fatalf("Compact = %v, %d Hub calls", err, len(e.hub.calls()))
		}
	})
	t.Run("another summary meanwhile", func(t *testing.T) {
		e := newEnv(t)
		e.chat(t, "c1", 21, false)
		other, err := values.ParseHistorySummary(10, "Другая сводка.")
		if err != nil {
			t.Fatal(err)
		}
		once := true
		e.chats.beforeSave = func() {
			if !once {
				return
			}
			once = false
			th := e.chats.stored(t, "c1")
			if err := th.Compact(other, e.clock.Now()); err != nil {
				t.Error(err)
			}
			if err := e.chats.Save(t.Context(), th); err != nil {
				t.Error(err)
			}
		}
		got, err := e.svc.Compact(t.Context(), "c1")
		onlyCode(t, err, ErrChanged)
		if !errors.Is(err, errRequestChanged) || got.Compacted || len(e.hub.calls()) != 1 {
			t.Fatalf("Compact = %+v, %v, %d Hub calls", got, err, len(e.hub.calls()))
		}
		if s := e.chats.stored(t, "c1").Summary(); s != other {
			t.Fatalf("stored %+v", s)
		}
	})
}

// A chat whose request is running is not compacted: no Hub call during a step.
func TestRunningRequestIsBusy(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 22, true)
	_, err := e.svc.Compact(t.Context(), "c1")
	onlyCode(t, err, ErrBusy)
	if !errors.Is(err, thread.ErrTurnInProgress) || len(e.hub.calls()) != 0 {
		t.Fatalf("Compact = %v, %d Hub calls", err, len(e.hub.calls()))
	}
}

// Two compactions of one chat at once make one Hub call: the second waits and finds the stored summary.
func TestConcurrentCompactionsCallTheHubOnce(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 21, false)
	e.hub.arrived, e.hub.hold = make(chan struct{}, 2), make(chan struct{})

	var wg sync.WaitGroup
	results := make([]Result, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { results[i], errs[i] = e.svc.Compact(t.Context(), "c1") })
	}
	<-e.hub.arrived
	close(e.hub.hold)
	wg.Wait()

	if errs[0] != nil || errs[1] != nil || results[0].Compacted == results[1].Compacted {
		t.Fatalf("results %+v, errors %v", results, errs)
	}
	if n := len(e.hub.calls()); n != 1 {
		t.Fatalf("%d Hub calls", n)
	}
	if results[0].Fill != results[1].Fill {
		t.Fatalf("fills differ: %+v", results)
	}
}

// An empty ID is the current chat; no current chat is an empty result.
func TestCurrentChat(t *testing.T) {
	e := newEnv(t)
	if got, err := e.svc.Compact(t.Context(), ""); err != nil || got != (Result{}) {
		t.Fatalf("no chat: %+v, %v", got, err)
	}
	e.chat(t, "old", 21, false)
	e.chat(t, "new", 21, false)
	got, err := e.svc.Compact(t.Context(), "")
	if err != nil || !got.Compacted {
		t.Fatalf("Compact = %+v, %v", got, err)
	}
	if calls := e.hub.calls(); len(calls) != 1 || calls[0].Thread() != "new" {
		t.Fatalf("compacted %v", calls)
	}
	if !e.chats.stored(t, "old").Summary().IsZero() {
		t.Fatal("the older chat was compacted")
	}
}

func TestInputAndStore(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Compact(t.Context(), "a/b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("malformed id = %v", err)
	}
	_, err := e.svc.Compact(t.Context(), "nope")
	onlyCode(t, err, ErrStore)
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing chat = %v", err)
	}

	// An empty chat holds no conversation: nothing read, nothing asked.
	th, err := thread.New("empty", e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	e.chats.put(t, th)
	if got, err := e.svc.Compact(t.Context(), "empty"); err != nil || got != (Result{Fill: Fill{Level: thread.FillNormal}}) {
		t.Fatalf("empty chat = %+v, %v", got, err)
	}
	if e.history.reads != 0 {
		t.Fatal("an empty chat read its history")
	}

	e.chat(t, "c1", 21, false)
	disk := errors.New("disk")
	e.history.err = disk
	_, err = e.svc.Compact(t.Context(), "c1")
	onlyCode(t, err, ErrStore)
	if !errors.Is(err, disk) || len(e.hub.calls()) != 0 {
		t.Fatalf("failed read = %v", err)
	}
}

// Neither a student's text nor a summary reaches an error text or the log.
func TestTextsStayOutOfErrorsAndLog(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "c1", 21, false)
	e.history.entries["c1"] = historyOf(21, func(n int) string { return fmt.Sprintf("СЕКРЕТ-студента %d", n) })
	e.hub.answer = answering("СЕКРЕТ-сводки\x07")

	_, err := e.svc.Compact(t.Context(), "c1")
	if err == nil {
		t.Fatal("an invalid summary was accepted")
	}
	e.hub.answer = func(int) (string, error) { return "", fmt.Errorf("hub: %w", shared.ErrNetworkUnavailable) }
	e.chat(t, "c2", 21, false)
	_, err2 := e.svc.Compact(t.Context(), "c2")

	logged := e.log.String()
	for _, s := range []string{err.Error(), err2.Error(), logged} {
		if strings.Contains(s, "СЕКРЕТ") {
			t.Fatalf("a text leaked: %s", s)
		}
	}
	if !strings.Contains(logged, "thread=c1") || !strings.Contains(logged, "thread=c2") || !strings.Contains(logged, ErrRefused.Error()) {
		t.Fatalf("log = %s", logged)
	}
}
