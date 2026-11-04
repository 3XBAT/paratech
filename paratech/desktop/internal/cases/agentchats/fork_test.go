package agentchats

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// pastOf is what the store reads of chat requests 1…n: one text each and a frozen line.
func (e *env) pastOf(id shared.ThreadID, texts ...string) {
	if e.chats.past == nil {
		e.chats.past = map[shared.ThreadID][]thread.PastRequest{}
	}
	at := e.clock.Now()
	for i, text := range texts {
		e.chats.past[id] = append(e.chats.past[id], thread.PastRequest{
			Number: i + 1, Texts: []string{text}, ReplyLine: fmt.Sprintf("reply: ответ %d", i+1), StartedAt: at, EndedAt: at,
		})
	}
}

// Fork writes only the new chat, with the copies before n and the generated id, returns it as the current chat and
// the text of n without its upload line; the source is read once and stays as it is.
func TestForkWritesOnlyTheNewChat(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("source running %v", running), func(t *testing.T) {
			e := newEnv(t)
			src := e.chat(t, "src", !running, "первая", "вторая", "третья")
			e.pastOf("src", "первая", "вторая\n[upload:u1]", "третья")
			before := e.chats.states["src"]
			saves := e.chats.saves

			got, err := e.svc.Fork(t.Context(), "src", 2)
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != "вторая" || got.Chat.ID != "th1" || got.Chat.Title != "первая (fork)" || got.Chat.Running ||
				got.Chat.Archived || got.Chat.Fill == nil {
				t.Fatalf("Fork = %+v", got)
			}
			if !reflect.DeepEqual(e.chats.states["src"], before) || src.Version() != before.Version {
				t.Fatalf("the source changed: %+v", e.chats.states["src"])
			}
			if e.chats.saves != saves+1 || len(e.chats.forks) != 1 || e.chats.forks[0].Thread.ID() != "th1" {
				t.Fatalf("saves %d → %d, forks %d", saves, e.chats.saves, len(e.chats.forks))
			}
			if want := []turnKey{{"src", 2}}; !reflect.DeepEqual(e.chats.reads, want) {
				t.Fatalf("ForkSource reads = %v", e.chats.reads)
			}
			fork := e.chats.states["th1"]
			if fork.Version != 1 || fork.ForkedFrom != "src" || fork.Turns != 1 || fork.Current.ReplyLine != "reply: ответ 1" {
				t.Fatalf("stored fork = %+v", fork)
			}
			cur, err := e.chats.Current(t.Context())
			if err != nil || cur.ID() != "th1" {
				t.Fatalf("current chat = %v, %v", cur, err)
			}
		})
	}
}

// Errors: a bad id or number never reads the store, a missing chat is not found, a request that cannot be forked from
// is the domain's error; nothing is written.
func TestForkErrors(t *testing.T) {
	storeDown := errors.New("disk")
	tests := []struct {
		name  string
		id    shared.ThreadID
		n     int
		setup func(e *env)
		want  error
		reads int
	}{
		{"bad id", "a/b", 1, nil, ErrInvalidInput, 0},
		{"n is 0", "src", 0, nil, ErrInvalidInput, 0},
		{"missing chat", "nope", 1, nil, shared.ErrNotFound, 0},
		{"after the last request", "src", 4, nil, thread.ErrInvalidFork, 1},
		{"hidden request", "src", 3, func(e *env) { e.chats.past["src"] = e.chats.past["src"][:2] }, thread.ErrInvalidFork, 1},
		{"read fails", "src", 2, func(e *env) { e.chats.forkErr = storeDown }, storeDown, 1},
		{"id taken", "src", 2, func(e *env) { e.chat(t, "th1", true, "чужой") }, shared.ErrVersionConflict, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "src", true, "первая", "вторая", "третья")
			e.pastOf("src", "первая", "вторая", "третья")
			if tt.setup != nil {
				tt.setup(e)
			}
			saves := e.chats.saves
			_, err := e.svc.Fork(t.Context(), tt.id, tt.n)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Fork = %v, want %v", err, tt.want)
			}
			if len(e.chats.reads) != tt.reads || len(e.chats.forks) != 0 || e.chats.saves != saves {
				t.Fatalf("reads %v, forks %d, saves %d → %d", e.chats.reads, len(e.chats.forks), saves, e.chats.saves)
			}
		})
	}
}
