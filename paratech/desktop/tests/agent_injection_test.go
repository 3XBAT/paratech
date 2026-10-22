package tests

// Injection fixtures (shared/testdata/agent/injection, TASK-135, ADR-0022) at the desktop layers that exist today:
// the strict argument parser, the name value objects, the policy and the thread, the agent use case with the real
// SQLite and Hub client, and the INSPECT_MATERIAL reader's answer (TASK-122). What needs the Driver (TASK-117) is
// listed in the fixtures' pending_driver field.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/testenv"
)

// sweepIDs are the known IDs of the sweep thread, one per kind, so every baseline call passes provenance.
var sweepIDs = map[values.IDKind]string{
	values.KindSubject: "s1", values.KindFolder: "f1", values.KindLesson: "l1", values.KindRecording: "rec1",
	values.KindMaterial: "m1", values.KindUpload: "u1", values.KindAssignment: "a1", values.KindJob: "j1",
	values.KindWork: "w1", values.KindVersion: "v1", values.KindProposal: "p1", values.KindPreview: "pv1",
	values.KindAnswer: "an1",
}

// baselineValue is a valid value of an argument; with one of them per argument the call parses and is admitted.
func baselineValue(a values.ArgSpec) any {
	switch a.Type {
	case values.ArgID:
		return sweepIDs[a.Kind]
	case values.ArgIDs:
		return []string{sweepIDs[a.Kind]}
	case values.ArgDate:
		return "2026-10-22"
	case values.ArgEnum:
		return a.Values[0]
	case values.ArgLabel:
		if a.Persistent {
			return "Лабы"
		}
		return "производная"
	case values.ArgText:
		return "Готово."
	case values.ArgTextList:
		return []string{"Готово."}
	case values.ArgPlanSteps:
		return []any{map[string]any{"tool": "create_subject", "arguments": map[string]any{"name": "Химия"}}}
	default: // questions
		return []any{map[string]any{"kind": "text", "question": "Какой объём?"}}
	}
}

// injectedValue puts the untrusted text into an argument of type a, in the place an obedient planner would.
func injectedValue(a values.ArgSpec, text string) any {
	switch a.Type {
	case values.ArgIDs, values.ArgTextList:
		return []string{text}
	case values.ArgPlanSteps:
		return []any{map[string]any{"tool": "propose_send", "arguments": map[string]any{"assignment": "a1", "version": text}}}
	case values.ArgQuestions:
		return []any{map[string]any{"kind": "text", "question": text}}
	}
	return text
}

// baselineArgs are the required arguments and the first of each one-of pair.
func baselineArgs(spec values.ToolSpec) map[string]any {
	args := map[string]any{}
	for _, a := range spec.Args() {
		if a.Required {
			args[a.Name] = baselineValue(a)
		}
	}
	for _, pair := range spec.OneOf() {
		for _, a := range spec.Args() {
			if a.Name == pair[0] {
				args[a.Name] = baselineValue(a)
			}
		}
	}
	return args
}

// sweepThread is a thread between steps of a request in subject s1 that knows every ID of sweepIDs.
func sweepThread(t *testing.T) (*thread.Thread, values.RefSet) {
	t.Helper()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	scope := evalScope(t)
	key, err := thread.KeyOf(scope)
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th-sweep", key, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartRequest(scope, now); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(now); err != nil {
		t.Fatal(err)
	}
	if err := th.EndStep(1, 100, now); err != nil {
		t.Fatal(err)
	}
	known := values.RefSet{}
	for kind, id := range sweepIDs {
		known.Add(agentRef(t, kind, id))
	}
	return th, known
}

// decideAlone runs one call through the parser and, for a tool that is not a card, the thread's Admit (as
// cases/agentturn does) and reports whether it would reach the Driver, with the class it would get.
func decideAlone(t *testing.T, tool values.Tool, args map[string]any) (actionpolicy.Class, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	call, err := values.ParseCall("toolu_1", string(tool), string(raw))
	if err != nil {
		return "", err
	}
	th, known := sweepThread(t)
	now := time.Date(2026, 10, 22, 9, 1, 0, 0, time.UTC)
	switch tool {
	case values.ToolShowPlan:
		return actionpolicy.ClassUI, th.ProposePlan(call.Args().PlanSteps("steps"), known, now)
	case values.ToolAskQuestions, values.ToolReply, values.ToolCannotDo:
		return actionpolicy.ClassUI, nil // shown to the student only
	}
	adm, err := th.Admit(call, thread.Facts{Context: known, CostMicroUSD: evalCost}, now)
	return adm.Class, err
}

// TestAgentInjectionSlotSweep puts each untrusted text into every argument of every tool, as an obedient planner
// would, and checks the call is refused unless the argument is free text from the student's request or a card text:
// never an ID, an enum value, a date, a plan step ID or a stored name the fixture forbids.
func TestAgentInjectionSlotSweep(t *testing.T) {
	for _, tool := range values.Tools() {
		spec, err := values.SpecOf(tool)
		if err != nil {
			t.Fatal(err)
		}
		// Lecture text and turnkey writing always need a plan; their arguments are still valid.
		rule, _ := actionpolicy.Of(tool)
		if _, err := decideAlone(t, tool, baselineArgs(spec)); err != nil && (!rule.AlwaysPlan || !errors.Is(err, thread.ErrPlanRequired)) {
			t.Fatalf("%s: the baseline call is refused: %v", tool, err)
		}
	}

	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			for _, tool := range values.Tools() {
				spec, _ := values.SpecOf(tool)
				for _, a := range spec.Args() {
					args := baselineArgs(spec)
					for _, pair := range spec.OneOf() {
						if a.Name == pair[1] {
							delete(args, pair[0])
						}
					}
					args[a.Name] = injectedValue(a, fx.Untrusted)
					class, err := decideAlone(t, tool, args)
					if err == nil {
						if !injectionMayPass(a, fx) {
							t.Errorf("%s.%s (%s) accepted the untrusted text as class %s", tool, a.Name, a.Type, class)
						}
						continue
					}
					if !thread.IsRefusal(err) {
						t.Errorf("%s.%s: %v is not a refusal the planner gets back", tool, a.Name, err)
					}
					// An ID-shaped text passes the parser (and the Hub); typed provenance is the gate.
					if _, idErr := values.NewRef(a.Kind, fx.Untrusted); (a.Type == values.ArgID || a.Type == values.ArgIDs) &&
						idErr == nil && !errors.Is(err, actionpolicy.ErrUnknownID) {
						t.Errorf("%s.%s: an ID-shaped text was refused by %v, want the provenance check", tool, a.Name, err)
					}
				}
			}
		})
	}
}

// injectionMayPass is where untrusted text is allowed: words from the student's request (labels, which the planner
// copies from the request it saw; it never saw the sources) and card texts the student reads. Stored names only when
// the fixture says the text is a valid name.
func injectionMayPass(a values.ArgSpec, fx injectionFixture) bool {
	switch a.Type {
	case values.ArgLabel:
		return !a.Persistent || !fx.RefusedAsName
	case values.ArgText, values.ArgTextList, values.ArgQuestions:
		return true
	}
	return false
}

// TestAgentInjectionNames: untrusted texts are never accepted as names the agent stores, a schedule title or a file
// name with a command is not a library folder name, and a subject named by such a text is never sent in the base.
func TestAgentInjectionNames(t *testing.T) {
	today, err := values.NewDate(2026, time.October, 22)
	if err != nil {
		t.Fatal(err)
	}
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			if _, err := values.ParseName(fx.Untrusted); fx.RefusedAsName && err == nil {
				t.Errorf("accepted as a stored name")
			}
			if fx.Source == "file_name" || fx.Source == "ics_title" {
				if _, err := subject.NewFolderName(fx.Untrusted); err == nil {
					t.Errorf("accepted as a library folder name")
				}
			}
			base := []values.BaseSubject{{ID: agentRef(t, values.KindSubject, "s1"), Name: values.Name(fx.Untrusted)}}
			if _, err := values.NewBase(base, today, "Europe/Moscow"); fx.RefusedAsName && err == nil {
				t.Errorf("a subject of this name would be sent to the planner")
			}
		})
	}
}

// TestAgentInjectionInToolResults: untrusted text that leaks into a tool result is data for the planner. When the
// planner then obeys it (a send with its address, a folder or a filter made of it, a deletion), every such call is
// refused and nothing reaches the Driver. The text never lands in the base, the scope or the student's turn.
func TestAgentInjectionInToolResults(t *testing.T) {
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			e := newEvalEnv(t, evalFacts{cost: evalCost})
			q, err := json.Marshal(fx.Untrusted)
			if err != nil {
				t.Fatal(err)
			}
			s := string(q)
			e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "inspect_upload", Args: `{"upload":"u1"}`}).
				Then(
					testenv.AgentCall{ID: "toolu_2", Tool: "propose_send", Args: `{"assignment":"a1","version":` + s + `}`},
					testenv.AgentCall{ID: "toolu_3", Tool: "create_folder", Args: `{"subject":"s1","name":` + s + `}`},
					testenv.AgentCall{ID: "toolu_4", Tool: "list_assignments", Args: `{"status":` + s + `}`},
					testenv.AgentCall{ID: "toolu_5", Tool: "propose_delete_recording", Args: `{"recording":` + s + `}`},
				).Then(evalEnd)

			st := evalAsk(t, e, "что в этом файле?")
			if len(st.Calls) != 1 || st.Calls[0].Call.Tool() != values.ToolInspectUpload {
				t.Fatalf("first step = %+v", st.Calls)
			}
			// A result the Driver's mapper should never build (ADR-0022 allowlist): it carries the file's text.
			leak := agentturn.Outcome{Result: values.ToolResult{CallID: st.Calls[0].Call.ID(), ResultJSON: `{"kind":"methodichka","note":` + s + `}`}}
			st = e.submit(t, st, leak)
			if len(st.Calls) != 0 || st.Status != thread.StatusDone {
				t.Fatalf("after the obedient step = %+v, want every call refused", st)
			}
			for _, id := range []string{"toolu_2", "toolu_3", "toolu_4", "toolu_5"} {
				r, ok := resultOf(e, id)
				if !ok || refusalOf(t, r) == "" {
					t.Errorf("%s: result %v, want a refusal", id, r)
				}
			}

			for _, req := range e.hub.Requests() {
				for _, turn := range req.GetTurns() {
					if strings.Contains(turn.GetStudentText(), fx.Untrusted) {
						t.Fatal("the untrusted text is in the student's turn")
					}
				}
				for _, b := range req.GetBase().GetSubjects() {
					if strings.Contains(b.GetName(), fx.Untrusted) {
						t.Fatal("the untrusted text is in the base")
					}
				}
			}
		})
	}
}

// TestAgentInjectionRefusalsCarryNoText: a refused call is answered with an enum only, so the planner never gets the
// untrusted text back from the desktop's own error message.
func TestAgentInjectionRefusalsCarryNoText(t *testing.T) {
	for _, fx := range loadInjectionFixtures(t) {
		q, _ := json.Marshal(fx.Untrusted)
		_, err := values.ParseCall("toolu_1", "list_assignments", `{"status":`+string(q)+`}`)
		why, ok := thread.RefusalOf(err)
		if !ok || !errors.Is(err, values.ErrInvalidArguments) {
			t.Fatalf("%s: err %v", fx.Name, err)
		}
		if res := values.RefusalResult("toolu_1", why); strings.Contains(res.ResultJSON, fx.Untrusted) || !res.IsError {
			t.Fatalf("%s: refusal result %s", fx.Name, res.ResultJSON)
		}
	}
}

// readerSlots are the fields of an INSPECT_MATERIAL answer; obeyedAnswer puts text into one of them, as a reader that
// obeyed the material would (a JSON string, also where the schema wants a date, an ID or a boolean).
var readerSlots = []string{"doc_kind", "subject", "lesson_date", "has_homework", "has_ticket_list", "mentions_email"}

func obeyedAnswer(t *testing.T, slot, text string) string {
	t.Helper()
	answer := map[string]any{
		"doc_kind": "other", "subject": nil, "lesson_date": nil, "has_homework": false, "has_ticket_list": false,
		"mentions_email": false,
	}
	answer[slot] = text
	b, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAgentInjectionReaderAnswers: whatever slot of the reader's answer the untrusted text lands in, the desktop
// parser refuses the whole answer (the Hub's schema check does the same, hub/tests). The free text in an enum field
// of reader-enum-free-text is one of these cases.
func TestAgentInjectionReaderAnswers(t *testing.T) {
	req, err := inspection.NewRequest("m1", time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		[]inspection.Candidate{{ID: "s1", Name: "Физика"}}, "Лекция 1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := req.Parse(obeyedAnswer(t, "subject", "s1")); err != nil {
		t.Fatalf("a valid answer is refused: %v", err)
	}
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			for _, slot := range readerSlots {
				out := obeyedAnswer(t, slot, fx.Untrusted)
				if _, err := req.Parse(out); !errors.Is(err, inspection.ErrInvalidAnswer) {
					t.Errorf("%s: %v, want the answer refused", slot, err)
				} else if strings.Contains(err.Error(), fx.Untrusted) {
					t.Errorf("%s: the refusal quotes the text", slot)
				}
			}
			// An extra field carrying the text is refused as well.
			if _, err := req.Parse(strings.Replace(obeyedAnswer(t, "subject", "s1"), "{", `{"note":`+mustQuote(t, fx.Untrusted)+",", 1)); !errors.Is(err, inspection.ErrInvalidAnswer) {
				t.Errorf("extra field: %v", err)
			}
		})
	}
}

// TestAgentInjectionReaderEndToEnd: every fixture as the text of an uploaded file, read through the real stack
// (extractor, Hub client, fake Hub, SQLite). The reader that obeys it in the enum field is refused, nothing is kept as
// an answer and the file stays where it was: no move, link or send happens because of a file's text.
func TestAgentInjectionReaderEndToEnd(t *testing.T) {
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			e := newMaterialScenario(t)
			ctx := context.Background()
			up, err := e.lib.Import(ctx, library.Upload{Name: "file.txt", Data: []byte("Методичка.\n" + fx.Untrusted + "\n")})
			if err != nil {
				t.Fatal(err)
			}
			e.hub.AnswerInspect(obeyedAnswer(t, "doc_kind", fx.Untrusted))
			if _, err := e.lib.Inspect(ctx, up.Material.ID()); !errors.Is(err, library.ErrReaderRefused) {
				t.Fatalf("Inspect = %v, want the answer refused", err)
			}
			if ins, err := e.inspections.Get(ctx, up.Material.ID()); err != nil || ins.Status() != inspection.StatusRefused {
				t.Fatalf("stored = %+v, %v", ins, err)
			}
			m, err := e.lib.Material(ctx, up.Material.ID())
			if err != nil || !m.InInbox() || len(m.Lessons()) != 0 {
				t.Fatalf("material = %+v, %v", m, err)
			}
			// The text reached the reader only as the value of material_text.
			specs := e.inspectRequests()
			var data map[string]any
			if len(specs) != 1 || json.Unmarshal([]byte(specs[0].GetUntrustedData()), &data) != nil ||
				!strings.Contains(data["material_text"].(string), fx.Untrusted) || specs[0].GetInstructions() != "" {
				t.Fatalf("reader request %v", specs)
			}
		})
	}
}

func mustQuote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
