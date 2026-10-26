package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

// TestCheckRefs: the IDs a reply lists need the same typed provenance as the IDs of a call: the context of the step
// or a tool result of this thread, never another thread's.
func TestCheckRefs(t *testing.T) {
	ctx := context(t)
	reply := func(t *testing.T, args string) []values.Ref {
		return call(t, "reply", args).Refs()
	}
	tests := []struct {
		name    string
		args    string
		seen    []values.Ref
		wantErr error
	}{
		{"no ids", `{"text":"Готово"}`, nil, nil},
		{"seen in a result", `{"text":"x","assignments":["a1"]}`, []values.Ref{ref(t, values.KindAssignment, "a1")}, nil},
		{"unknown", `{"text":"x","assignments":["a9"]}`, []values.Ref{ref(t, values.KindAssignment, "a1")}, actionpolicy.ErrUnknownID},
		{"seen as another kind", `{"text":"x","assignments":["u1"]}`, []values.Ref{ref(t, values.KindUpload, "u1")}, actionpolicy.ErrUnknownID},
		{"one unknown of two", `{"text":"x","assignments":["a1","a2"]}`, []values.Ref{ref(t, values.KindAssignment, "a1")}, actionpolicy.ErrUnknownID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			if err := th.Observe(tt.seen...); err != nil {
				t.Fatal(err)
			}
			err := th.CheckRefs(reply(t, tt.args), ctx)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CheckRefs = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if why, ok := RefusalOf(err); !ok || why != values.RefuseUnknownID {
					t.Fatalf("refusal = %v %v, want unknown_id", why, ok)
				}
			}
		})
	}

	t.Run("in the context of the step", func(t *testing.T) {
		th := newThread(t)
		a := ref(t, values.KindAssignment, "a5")
		refs := reply(t, `{"text":"x","assignments":["a5"]}`)
		if err := th.CheckRefs(refs, ctx); !errors.Is(err, actionpolicy.ErrUnknownID) {
			t.Fatalf("outside the context = %v", err)
		}
		if err := th.CheckRefs(refs, values.NewRefSet(a)); err != nil {
			t.Fatalf("in the context = %v", err)
		}
	})

	t.Run("seen in an earlier request of the thread", func(t *testing.T) {
		th := newThread(t)
		if err := th.Observe(ref(t, values.KindAssignment, "a1")); err != nil {
			t.Fatal(err)
		}
		if err := th.Complete(at(2)); err != nil {
			t.Fatal(err)
		}
		if err := th.StartTurn(at(3)); err != nil {
			t.Fatal(err)
		}
		if err := th.CheckRefs(reply(t, `{"text":"x","assignments":["a1"]}`), ctx); err != nil {
			t.Fatalf("CheckRefs = %v", err)
		}
	})

	t.Run("seen in another thread", func(t *testing.T) {
		earlier := newThread(t)
		if err := earlier.Observe(ref(t, values.KindAssignment, "a1")); err != nil {
			t.Fatal(err)
		}
		th, err := New("th2", NoSubject, at(5))
		if err != nil {
			t.Fatal(err)
		}
		if err := th.StartTurn(at(6)); err != nil {
			t.Fatal(err)
		}
		if err := th.CheckRefs(reply(t, `{"text":"x","assignments":["a1"]}`), ctx); !errors.Is(err, actionpolicy.ErrUnknownID) {
			t.Fatalf("CheckRefs = %v", err)
		}
	})
}
