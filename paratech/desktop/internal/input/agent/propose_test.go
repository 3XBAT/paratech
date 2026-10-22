package agent

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// fakeProposals records what the C tools asked for. It can only propose: there is nothing to confirm on it.
type fakeProposals struct {
	mu       sync.Mutex
	calls    []string
	deadline func(a shared.AssignmentID) (homeworkflow.ProposalView, error)
	email    func(a shared.AssignmentID, n int) (homeworkflow.ProposalView, error)
}

func (f *fakeProposals) ProposeDeadline(_ context.Context, a shared.AssignmentID) (homeworkflow.ProposalView, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "deadline "+string(a))
	f.mu.Unlock()
	if f.deadline != nil {
		return f.deadline(a)
	}
	return homeworkflow.ProposalView{ID: "p-" + proposal.ID(a), Kind: proposal.KindDeadlineEvent, Status: proposal.StatusValidated}, nil
}

func (f *fakeProposals) ProposeEmailOfVersion(_ context.Context, a shared.AssignmentID, n int) (homeworkflow.ProposalView, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("email %s v%d", a, n))
	f.mu.Unlock()
	if f.email != nil {
		return f.email(a, n)
	}
	return homeworkflow.ProposalView{
		ID: "p-mail", Kind: proposal.KindSendHomework, Status: proposal.StatusValidated, Recipient: "teacher@school.example",
	}, nil
}

func (f *fakeProposals) made() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeDeletions previews recordings: rec1 can go, rec2 is still recording, anything else is gone.
type fakeDeletions struct{ previews int }

func (f *fakeDeletions) Preview(_ context.Context, id shared.RecordingID) (recordingdelete.Preview, error) {
	f.previews++
	switch id {
	case "rec1":
		return recordingdelete.Preview{Recording: id}, nil
	case "rec2":
		return recordingdelete.Preview{Recording: id, Refusal: recordingdelete.RefusalInProgress}, nil
	}
	return recordingdelete.Preview{}, fmt.Errorf("preview %s: %w", id, recordingdelete.ErrNotFound)
}

// proposeCall is an admitted C call as agentturn returns it.
func proposeCall(t *testing.T, id string, tool values.Tool, args string) agentturn.Call {
	t.Helper()
	return agentturn.Call{Call: mustCall(t, id, tool, args), Class: actionpolicy.ClassC, PlanStep: -1}
}

func proposeWorld(t *testing.T) (*world, *fakeProposals, *fakeDeletions, tools) {
	t.Helper()
	w := newWorld(t)
	p, d := &fakeProposals{}, &fakeDeletions{}
	deps := w.deps(&scriptedTurns{}, &chanSink{})
	deps.Proposals, deps.Deletions = p, d
	drv, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	return w, p, d, drv.tools
}

// Every C tool makes a proposal or a preview, journals it PROPOSED and shows a confirm card bound to it; nothing
// irreversible has a way to run (ADR-0021).
func TestProposalToolsMakeACardOnly(t *testing.T) {
	tests := []struct {
		name   string
		tool   values.Tool
		args   string
		made   string
		target values.Ref
		result string
	}{
		{"deadline", values.ToolProposeDeadline, `{"assignment":"a1"}`, "deadline a1", mustRef(t, values.KindProposal, "p-a1"),
			`{"proposal":"p-a1","proposed":true,"waits_for":"student"}`},
		{"send", values.ToolProposeSend, `{"assignment":"a1","version":"v2"}`, "email a1 v2", mustRef(t, values.KindProposal, "p-mail"),
			`{"proposal":"p-mail","proposed":true,"waits_for":"student"}`},
		{"delete recording", values.ToolProposeDeleteRecording, `{"recording":"rec1"}`, "", mustRef(t, values.KindPreview, "rec1"),
			`{"preview":"rec1","proposed":true,"waits_for":"student"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, p, d, tl := proposeWorld(t)
			c := proposeCall(t, "c1", tt.tool, tt.args)
			r := tl.run(t.Context(), req, c)
			if r.out.Result.IsError || r.out.Result.ResultJSON != tt.result || !r.write {
				t.Fatalf("result = %+v", r.out)
			}
			if len(r.out.Seen) != 1 || r.out.Seen[0] != tt.target {
				t.Fatalf("seen = %v", r.out.Seen)
			}
			if len(r.cards) != 1 || r.cards[0] != (values.ConfirmCard{Target: tt.target}) {
				t.Fatalf("cards = %+v", r.cards)
			}
			e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1")
			if err != nil || e.Status != action.StatusProposed || e.Proposal != tt.target || e.CanUndo ||
				e.NoUndo != action.NoUndoIrreversible {
				t.Fatalf("journal = %+v, %v", e, err)
			}
			if tt.made != "" && (len(p.made()) != 1 || p.made()[0] != tt.made) {
				t.Fatalf("use case calls = %v", p.made())
			}
			if tt.made == "" && d.previews != 1 {
				t.Fatalf("previews = %d", d.previews)
			}

			// The step replayed (a crash, «Продолжить»): the same answer, no second proposal, no second card.
			again := tl.run(t.Context(), req, c)
			if again.out.Result != r.out.Result || len(again.cards) != 0 || len(p.made()) > 1 || d.previews > 1 {
				t.Fatalf("replay = %+v, calls %v, previews %d", again, p.made(), d.previews)
			}
		})
	}
}

// What a proposal tool cannot do is an enum for the planner, no journal line and no card.
func TestProposalToolsRefuse(t *testing.T) {
	notFound := fmt.Errorf("assignment: %w", shared.ErrNotFound)
	tests := []struct {
		name  string
		tool  values.Tool
		args  string
		setup func(p *fakeProposals)
		want  values.Refusal
	}{
		{"no deadline", values.ToolProposeDeadline, `{"assignment":"a1"}`, func(p *fakeProposals) {
			p.deadline = func(shared.AssignmentID) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, homeworkflow.ErrNoDue
			}
		}, values.RefuseNoDue},
		{"deadline in the past", values.ToolProposeDeadline, `{"assignment":"a1"}`, func(p *fakeProposals) {
			p.deadline = func(shared.AssignmentID) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{ID: "p1", Status: proposal.StatusRejected, Reason: proposal.ReasonDueOutOfRange}, nil
			}
		}, values.RefuseDueOutOfRange},
		{"assignment gone", values.ToolProposeDeadline, `{"assignment":"a1"}`, func(p *fakeProposals) {
			p.deadline = func(shared.AssignmentID) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, notFound
			}
		}, values.RefuseNotFound},
		{"no recipient in the settings", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`, func(p *fakeProposals) {
			p.email = func(shared.AssignmentID, int) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{ID: "p1", Status: proposal.StatusRejected, Reason: proposal.ReasonNoRecipient}, nil
			}
		}, values.RefuseNoRecipient},
		{"version not exported", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`, func(p *fakeProposals) {
			p.email = func(shared.AssignmentID, int) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, homeworkflow.ErrVersionNotExported
			}
		}, values.RefuseNotExported},
		{"nothing exported", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`, func(p *fakeProposals) {
			p.email = func(shared.AssignmentID, int) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{ID: "p1", Status: proposal.StatusRejected, Reason: proposal.ReasonNoArtifact}, nil
			}
		}, values.RefuseNotExported},
		{"not a version id", values.ToolProposeSend, `{"assignment":"a1","version":"draft-2"}`, nil, values.RefuseInvalidArgs},
		{"use case failed", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`, func(p *fakeProposals) {
			p.email = func(shared.AssignmentID, int) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, errors.New("disk")
			}
		}, values.RefuseFailed},
		{"recording still recording", values.ToolProposeDeleteRecording, `{"recording":"rec2"}`, nil, values.RefuseNotDeletable},
		{"recording gone", values.ToolProposeDeleteRecording, `{"recording":"rec9"}`, nil, values.RefuseNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, p, _, tl := proposeWorld(t)
			if tt.setup != nil {
				tt.setup(p)
			}
			r := tl.run(t.Context(), req, proposeCall(t, "c1", tt.tool, tt.args))
			if !r.out.Result.IsError || !strings.Contains(r.out.Result.ResultJSON, `"`+string(tt.want)+`"`) || len(r.cards) != 0 {
				t.Fatalf("result = %+v, cards %v; want %s", r.out.Result, r.cards, tt.want)
			}
			if _, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); !errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("a refused proposal was journaled: %v", err)
			}
		})
	}
}

// Without the use cases or the journal a C tool answers "unavailable"; it is never run as a read or an action.
func TestProposalToolsUnavailable(t *testing.T) {
	plain := newReads().tools()
	for _, tool := range []values.Tool{values.ToolProposeDeadline, values.ToolProposeSend, values.ToolProposeDeleteRecording} {
		args := map[values.Tool]string{
			values.ToolProposeDeadline: `{"assignment":"a1"}`, values.ToolProposeSend: `{"assignment":"a1","version":"v1"}`,
			values.ToolProposeDeleteRecording: `{"recording":"rec1"}`,
		}[tool]
		r := plain.run(t.Context(), req, proposeCall(t, "c1", tool, args))
		if !strings.Contains(r.out.Result.ResultJSON, string(values.RefuseUnavailable)) || len(r.cards) != 0 {
			t.Errorf("%s without its use case = %+v", tool, r.out.Result)
		}
	}
}

// A call ID that journaled one proposal and comes back as another tool is refused, never answered with the other's
// proposal.
func TestProposalCallIDReusedForAnotherTool(t *testing.T) {
	_, p, _, tl := proposeWorld(t)
	tl.run(t.Context(), req, proposeCall(t, "c1", values.ToolProposeDeadline, `{"assignment":"a1"}`))
	r := tl.run(t.Context(), req, proposeCall(t, "c1", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`))
	if !strings.Contains(r.out.Result.ResultJSON, string(values.RefuseInvalidArgs)) || len(p.made()) != 1 {
		t.Fatalf("reused call id = %+v, calls %v", r.out.Result, p.made())
	}
}

// list_assignments shows the latest draft version, the only way propose_send gets a version it may name.
func TestListAssignmentsShowsTheLatestDraftVersion(t *testing.T) {
	r := newReads()
	r.assignments.views = []homeworkflow.AssignmentView{{ID: "a1"}, {ID: "a2"}}
	d := r.deps(&scriptedTurns{}, &chanSink{})
	d.Drafts = fakeDrafts{"a1": {{Number: 1}, {Number: 2, Latest: true}}}
	drv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	out := drv.tools.read(t.Context(), values.Scope{}, readCall(t, "c1", values.ToolListAssignments, `{}`))
	if !strings.Contains(out.Result.ResultJSON, `{"id":"a1","status":"no_due","version":"v2"}`) ||
		!strings.Contains(out.Result.ResultJSON, `{"id":"a2","status":"no_due"}`) {
		t.Fatalf("result = %s", out.Result.ResultJSON)
	}
	if !values.NewRefSet(out.Seen...).Has(mustRef(t, values.KindVersion, "v2")) {
		t.Fatalf("seen = %v", out.Seen)
	}
}

type fakeDrafts map[shared.AssignmentID][]homeworkflow.DraftVersionView

func (f fakeDrafts) DraftVersions(_ context.Context, a shared.AssignmentID) ([]homeworkflow.DraftVersionView, error) {
	return f[a], nil
}

// The agent has no path to an irreversible action (ADR-0021, ADR-0022, TASK-127): no interface of input/agent has a
// method that confirms, executes, sends, declines or deletes, and no production file of the package selects a method
// of the user's confirmation or of a side-effect port. go/parser has no types, so the check is by name; a false
// positive is resolved by renaming, never by an exception.
func TestAgentHasNoPathToIrreversibleActions(t *testing.T) {
	report := func(t *testing.T, got []string) {
		t.Helper()
		for _, g := range got {
			t.Error(g)
		}
	}
	got, err := irreversiblePaths(".")
	if err != nil {
		t.Fatal(err)
	}
	report(t, got)
}

// Control for the checker: an interface method and a call of each kind are found.
func TestIrreversiblePathsCheckerFindsThem(t *testing.T) {
	dir := t.TempDir()
	src := "package agent\n\ntype X interface{ ConfirmSend() }\n\nfunc f(s s) { s.Delete(); s.UpsertDeadline(); _ = s.MarkSent }\n" +
		"func g(v any) { _ = v.(interface{ ExecuteNow() }) }\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad_test.go"), []byte("package agent\n\nfunc h(s s) { s.Confirm() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := irreversiblePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"ConfirmSend", "Delete", "UpsertDeadline", "MarkSent", "ExecuteNow"} {
		if !strings.Contains(joined, want) {
			t.Errorf("not found: %s\n%s", want, joined)
		}
	}
	if len(got) != 5 {
		t.Errorf("got %d findings, want 5 (test files are skipped):\n%s", len(got), joined)
	}
}

// forbiddenInMethodNames are the words no interface method of the agent may contain.
var forbiddenInMethodNames = []string{"Confirm", "Execute", "Send", "Decline", "Delete", "MarkSent"}

// forbiddenCalls are the methods of the user's confirmation and of the side-effect ports.
var forbiddenCalls = map[string]bool{
	"Confirm": true, "ConfirmSend": true, "ConfirmDeadline": true, "ConfirmOnce": true, "Execute": true, "Decline": true,
	"Delete": true, "DeleteMany": true, "SendHomework": true, "SendAgain": true, "MarkSent": true, "UpsertDeadline": true,
	"ExportHomework": true, "ExportDraft": true, "ExportDocx": true, "SaveDocx": true, "MarkExecuted": true,
}

func irreversiblePaths(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.InterfaceType:
				for _, m := range n.Methods.List {
					for _, id := range m.Names {
						for _, word := range forbiddenInMethodNames {
							if strings.Contains(id.Name, word) {
								out = append(out, fmt.Sprintf("%s: interface method %s", fset.Position(id.Pos()), id.Name))
								break
							}
						}
					}
				}
			case *ast.SelectorExpr:
				if forbiddenCalls[n.Sel.Name] {
					out = append(out, fmt.Sprintf("%s: selects %s", fset.Position(n.Pos()), n.Sel.Name))
				}
			}
			return true
		})
	}
	return out, nil
}
