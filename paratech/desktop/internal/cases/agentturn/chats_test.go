package agentturn

import (
	"context"
	"errors"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeMentioned knows which objects exist; it records what it was asked.
type fakeMentioned struct {
	mu     sync.Mutex
	exists values.RefSet
	err    error
	asked  []values.Ref
}

func (f *fakeMentioned) Exists(_ context.Context, r values.Ref) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, r)
	return f.exists.Has(r), f.err
}

// withMentions rebuilds the service of e with a Mentions port (nil: none).
func withMentions(t *testing.T, e *env, m Mentioned) {
	t.Helper()
	d := Deps{
		Model: e.model, Threads: e.threads, History: e.history, Subjects: e.subjects, Facts: zeroFacts{},
		Tx: memTx{h: e.history, threads: e.threads}, Clock: e.clock, IDs: &seqIDs{}, Effects: e.effects,
	}
	d.Mentions = m
	svc, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
}

func mention(t *testing.T, kind values.IDKind, id, label string) values.Mention {
	t.Helper()
	m, err := values.NewMention(mustRef(t, kind, id), label)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func storedThread(t *testing.T, e *env, id shared.ThreadID) *thread.Thread {
	t.Helper()
	th, err := e.threads.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// A mentioned object that exists is kept with the request and its ID passes the provenance check; one that no longer
// exists is dropped without an error and its ID stays unknown to the planner's calls.
func TestAskKeepsExistingMentions(t *testing.T) {
	kept, gone := mention(t, values.KindSubject, "s7", "Матан"), mention(t, values.KindSubject, "s404", "Удалённый")
	tests := []struct {
		name     string
		port     *fakeMentioned
		wantKept bool
	}{
		{"checked", &fakeMentioned{exists: values.NewRefSet(kept.Ref())}, true},
		{"no port keeps none", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			var port Mentioned // a nil *fakeMentioned must not become a non-nil port
			if tt.port != nil {
				port = tt.port
			}
			withMentions(t, e, port)
			e.model.then(raw("c1", "find_lessons", `{"subject":"s7"}`), raw("c2", "find_lessons", `{"subject":"s404"}`))
			st, err := e.svc.Ask(t.Context(), AskInput{Text: "пары по матану", Mentions: []values.Mention{kept, gone}})
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}

			turn, _ := storedThread(t, e, st.Thread).Turn()
			ms := turn.Mentions()
			if tt.wantKept {
				if len(ms) != 1 || ms[0] != kept {
					t.Fatalf("stored mentions = %+v, want only the existing one", ms)
				}
				if len(st.Calls) != 1 || st.Calls[0].Call.ID() != "c1" {
					t.Fatalf("calls = %+v, want only the call with the kept mention", st.Calls)
				}
				if len(tt.port.asked) != 2 {
					t.Fatalf("asked = %v", tt.port.asked)
				}
				return
			}
			if len(ms) != 0 || len(st.Calls) != 0 {
				t.Fatalf("without a port: mentions %+v, calls %+v; want none", ms, st.Calls)
			}
		})
	}
}

func TestAskMentionChecks(t *testing.T) {
	many := make([]values.Mention, values.MaxMentions+1)
	for i := range many {
		many[i] = mention(t, values.KindMaterial, "m"+string(rune('a'+i)), "Файл")
	}
	boom := errors.New("db down")
	tests := []struct {
		name     string
		mentions []values.Mention
		portErr  error
		want     error
	}{
		{"too many", many, nil, ErrInvalidInput},
		{"zero mention", []values.Mention{{}}, nil, ErrInvalidInput},
		{"check fails", []values.Mention{many[0]}, boom, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			withMentions(t, e, &fakeMentioned{exists: values.NewRefSet(), err: tt.portErr})
			if _, err := e.svc.Ask(t.Context(), AskInput{Text: "x", Mentions: tt.mentions}); !errors.Is(err, tt.want) {
				t.Fatalf("Ask = %v, want %v", err, tt.want)
			}
			if len(e.threads.states) != 0 || len(e.model.requests()) != 0 {
				t.Fatal("a refused Ask stored a chat or reached the model")
			}
		})
	}
}

// Ask with a chat ID goes to that chat although another is current; an archived chat comes back from the archive; the
// first text names a new chat.
func TestAskIntoANamedChat(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"ок"}`)).then(raw("r2", "reply", `{"text":"ок"}`)).
		then(raw("r3", "reply", `{"text":"ок"}`))
	first := ask(t, e, "какие пары по физике")
	if got := storedThread(t, e, first.Thread).Title(); got != "какие пары по физике" {
		t.Fatalf("title = %q, want the first text", got)
	}

	newer, err := thread.New("th9", e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), newer); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: first.Thread, Text: "ещё"})
	if err != nil || st.Thread != first.Thread || st.Turn != 2 {
		t.Fatalf("Ask into the older chat = %+v, %v", st, err)
	}
	if _, ok := storedThread(t, e, "th9").Turn(); ok {
		t.Fatal("the current chat got the request")
	}

	th := storedThread(t, e, first.Thread)
	if err := th.Archive(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}
	st, err = e.svc.Ask(t.Context(), AskInput{ThreadID: first.Thread, Text: "продолжим"})
	if err != nil || st.Thread != first.Thread || st.Turn != 3 {
		t.Fatalf("Ask into an archived chat = %+v, %v", st, err)
	}
	if storedThread(t, e, first.Thread).Archived() {
		t.Fatal("a request in an archived chat left it in the archive")
	}
}

func TestAskNamedChatErrors(t *testing.T) {
	tests := []struct {
		name string
		id   shared.ThreadID
		want error
	}{
		{"missing", "th404", shared.ErrNotFound},
		{"malformed", "a/b", ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if _, err := e.svc.Ask(t.Context(), AskInput{ThreadID: tt.id, Text: "x"}); !errors.Is(err, tt.want) {
				t.Fatalf("Ask = %v, want %v", err, tt.want)
			}
			if len(e.threads.states) != 0 {
				t.Fatal("a chat was created")
			}
		})
	}
}

// Provenance is per chat: an ID known in one chat is refused in another.
func TestProvenanceIsPerChat(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"ок"}`)).then(raw("c1", "inspect_upload", `{"upload":"u1"}`)).
		then(raw("r2", "reply", `{"text":"ок"}`))
	first, err := e.svc.Ask(t.Context(), AskInput{Text: "вот файл", Uploads: []values.Ref{mustRef(t, values.KindUpload, "u1")}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := thread.New("th9", e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: "th9", Text: "посмотри u1"})
	if err != nil || st.Thread == first.Thread || len(st.Calls) != 0 {
		t.Fatalf("other chat = %+v, %v; want the call refused", st, err)
	}
	if got := lastResults(t, e); !got[0].IsError || got[0].ResultJSON != `{"error":"unknown_id"}` {
		t.Fatalf("result = %+v, want unknown_id", got)
	}
}
