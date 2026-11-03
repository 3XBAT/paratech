package agent

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// offerIDs are the IDs the offer tests name, one per kind a button takes.
var offerIDs = map[values.IDKind]string{
	values.KindMaterial: "m1", values.KindLesson: "l1", values.KindRecording: "r1", values.KindWork: "w1",
}

// offerArgs are the arguments of a button with the ID it needs.
func offerArgs(t *testing.T, a values.OfferAction) (string, values.Ref) {
	t.Helper()
	arg, kind, ok := values.OfferTarget(a)
	if !ok {
		t.Fatalf("unknown button %s", a)
	}
	if arg == "" {
		return fmt.Sprintf(`{"action":%q}`, a), values.Ref{}
	}
	r, err := values.NewRef(kind, offerIDs[kind])
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"action":%q,%q:%q}`, a, arg, r.ID()), r
}

func offerCall(t *testing.T, args string) agentturn.Call {
	t.Helper()
	c, err := values.ParseCallAt("c1", string(values.ToolOfferButton), args, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	return agentturn.Call{Call: c, Class: actionpolicy.ClassUI, PlanStep: -1, Card: values.CardOfferButton}
}

// TestOfferButtonEveryAction: every button of the closed enum makes its card with exactly the ID it needs, and the
// planner gets only that it is shown; the call link buttons answer unavailable until their binding (TASK-261/328).
func TestOfferButtonEveryAction(t *testing.T) {
	w := newWorld(t)
	tl := w.tools(t)
	for _, a := range values.OfferActions() {
		t.Run(string(a), func(t *testing.T) {
			args, target := offerArgs(t, a)
			r := tl.run(t.Context(), req, offerCall(t, args))
			if r.write {
				t.Error("a button is no action")
			}
			if a == values.OfferOpenCallLink || a == values.OfferCopyCallLink {
				if why, ok := values.RefusalOf(r.out.Result); !ok || why != values.RefuseUnavailable || len(r.cards) != 0 {
					t.Fatalf("call link: %+v, cards %v", r.out.Result, r.cards)
				}
				return
			}
			if r.out.Result != values.ShownResult("c1") || len(r.out.Seen) != 0 {
				t.Fatalf("result = %+v, seen %v", r.out.Result, r.out.Seen)
			}
			want := values.OfferButtonCard{Action: a, Target: target}
			if len(r.cards) != 1 || r.cards[0] != want {
				t.Fatalf("cards = %#v, want %#v", r.cards, want)
			}
		})
	}
	if _, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); err == nil {
		t.Error("a button is journaled")
	}
}

// TestOfferButtonTakesNoURLPathOrLabel: the card holds only the action and a typed ID, the result is the fixed
// «shown», and a call with any other argument (a URL, a path, a label, another ID) never makes a card.
func TestOfferButtonTakesNoURLPathOrLabel(t *testing.T) {
	ct := reflect.TypeFor[values.OfferButtonCard]()
	var fields []string
	for i := range ct.NumField() {
		fields = append(fields, ct.Field(i).Name+" "+ct.Field(i).Type.String())
	}
	if !slices.Equal(fields, []string{"Action values.OfferAction", "Target values.Ref"}) {
		t.Fatalf("OfferButtonCard fields = %v", fields)
	}

	for _, args := range []string{
		`{"action":"open_help","url":"https://example.invalid/x"}`,
		`{"action":"open_material","material":"m1","path":"C:/Users/x/file.docx"}`,
		`{"action":"save_as","work":"w1","label":"Жми сюда"}`,
		`{"action":"open_material","material":"m1","lesson":"l1"}`,
		`{"action":"undo_action","work":"w1"}`,
		`{"action":"open_url"}`,
	} {
		if _, err := values.ParseCallAt("c1", string(values.ToolOfferButton), args, values.CatalogWave10); !errors.Is(err, values.ErrInvalidArguments) {
			t.Errorf("%s: err = %v, want ErrInvalidArguments", args, err)
		}
	}

	// Another UI tool that reaches the Driver makes no card.
	reply, err := values.ParseCallAt("c1", string(values.ToolReply), `{"text":"Готово"}`, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	r := cardTool(agentturn.Call{Call: reply, Class: actionpolicy.ClassUI, PlanStep: -1})
	if why, ok := values.RefusalOf(r.out.Result); !ok || why != values.RefuseUnavailable || len(r.cards) != 0 {
		t.Fatalf("reply through the Driver: %+v, cards %v", r.out.Result, r.cards)
	}
}

// TestOfferButtonForeignIDRefused: the ID of a button passes the provenance check of its kind; one the chat has not
// seen (or seen as another kind) is unknown_id and never runs. A seen one is admitted as a UI call with its card.
func TestOfferButtonForeignIDRefused(t *testing.T) {
	newThread := func(t *testing.T) *thread.Thread {
		t.Helper()
		th, err := thread.New(th1, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := th.StartRequest(values.Scope{}, now); err != nil {
			t.Fatal(err)
		}
		if _, err := th.BeginStep(now); err != nil {
			t.Fatal(err)
		}
		if err := th.EndStep(1, 0, now); err != nil {
			t.Fatal(err)
		}
		return th
	}
	for _, a := range values.OfferActions() {
		args, target := offerArgs(t, a)
		if target.IsZero() {
			continue
		}
		t.Run(string(a), func(t *testing.T) {
			call := offerCall(t, args).Call

			th := newThread(t)
			_, err := th.Admit(call, thread.Facts{}, now)
			if why, ok := thread.RefusalOf(err); !ok || why != values.RefuseUnknownID {
				t.Fatalf("unseen id: err = %v", err)
			}

			th = newThread(t)
			wrongKind, err := values.NewRef(values.KindAssignment, target.ID())
			if err != nil {
				t.Fatal(err)
			}
			if err := th.Observe(wrongKind); err != nil {
				t.Fatal(err)
			}
			if _, err := th.Admit(call, thread.Facts{}, now); !errors.Is(err, actionpolicy.ErrUnknownID) {
				t.Fatalf("id seen as another kind: err = %v", err)
			}

			th = newThread(t)
			if err := th.Observe(target); err != nil {
				t.Fatal(err)
			}
			adm, err := th.Admit(call, thread.Facts{}, now)
			if err != nil || adm.Class != actionpolicy.ClassUI || adm.Card != values.CardOfferButton {
				t.Fatalf("seen id: %+v, %v", adm, err)
			}
		})
	}
}
