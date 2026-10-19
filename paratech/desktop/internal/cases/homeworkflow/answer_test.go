package homeworkflow

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// TASK-086: the owner's gateway answers with fences, prose or a cut-off object. The checks unwrap what they can, an
// answer that still fails is asked again with a fixed reminder, at most answerCalls calls per job run.
func TestInvalidAnswerIsAskedAgain(t *testing.T) {
	const cut = `{"summary":"The lesson was about`
	tests := []struct {
		name      string
		op        values.Operation
		typ       job.Type
		answers   []string
		wantErr   error // nil: stored
		wantCalls int
		reminder  []string // the reminder each repeat must carry
	}{
		{name: "wrapped answer passes at once", op: values.OperationSummary, typ: SummarizeJobType,
			answers: []string{"Here you go:\n```json\n" + summaryAnswer + "\n```"}, wantCalls: 1},
		{name: "second call passes", op: values.OperationSummary, typ: SummarizeJobType,
			answers: []string{"Rivers flow.", summaryAnswer}, wantCalls: 2, reminder: []string{"could not be read"}},
		{name: "third call passes after a cut", op: values.OperationExtractHomework, typ: ExtractHomeworkJobType,
			answers: []string{`{"assignments":[{"title":"x"`, "no", extractAnswer}, wantCalls: 3,
			reminder: []string{"cut off", "could not be read"}},
		{name: "always invalid", op: values.OperationSummary, typ: SummarizeJobType,
			answers: []string{"no JSON here"}, wantErr: ErrInvalidAnswer, wantCalls: 3,
			reminder: []string{"could not be read", "could not be read"}},
		{name: "always cut off", op: values.OperationSummary, typ: SummarizeJobType,
			answers: []string{cut}, wantErr: ErrTruncatedAnswer, wantCalls: 3, reminder: []string{"cut off", "cut off"}},
		{name: "cut off, then invalid", op: values.OperationExtractHomework, typ: ExtractHomeworkJobType,
			answers: []string{`{"assignments":[`, "no"}, wantErr: ErrInvalidAnswer, wantCalls: 3,
			reminder: []string{"cut off", "could not be read"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPPEnv(t)
			for _, a := range tc.answers {
				e.llm.Answer(tc.op, a)
			}
			err := e.handle(e.snapshot("j1", tc.typ, "t1"))

			reqs := e.llm.Requests()
			if len(reqs) != tc.wantCalls {
				t.Fatalf("LLM calls = %d, want %d", len(reqs), tc.wantCalls)
			}
			base := prompts.Instructions(tc.op)
			keys := map[string]bool{}
			for i, r := range reqs {
				keys[r.IdempotencyKey()] = true
				if r.IdempotencyKey() != llmKey("j1", r) || r.UntrustedData() != reqs[0].UntrustedData() {
					t.Fatalf("call %d: key %q or data differ from the first request", i+1, r.IdempotencyKey())
				}
				if i == 0 {
					if r.Instructions() != base {
						t.Fatal("the first call must carry the plain instructions")
					}
					continue
				}
				extra := strings.TrimPrefix(r.Instructions(), base)
				if extra == r.Instructions() || !strings.Contains(extra, tc.reminder[i-1]) {
					t.Fatalf("call %d: reminder %q, want one containing %q", i+1, extra, tc.reminder[i-1])
				}
				if strings.Contains(extra, "rivers") || strings.Contains(extra, "assignments\":[") {
					t.Fatal("the reminder echoes the model answer")
				}
			}
			if len(keys) != len(reqs) {
				t.Fatalf("keys %v: every call needs its own key", keys)
			}

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Handle = %v", err)
				}
				if e.records() == 0 {
					t.Fatal("nothing stored")
				}
				return
			}
			if !errors.Is(err, tc.wantErr) || backoff.Classify(err) != backoff.ClassPermanent ||
				err.Error() != backoff.Permanent(tc.wantErr).Error() {
				t.Fatalf("Handle = %v, want a permanent %v with the fixed text", err, tc.wantErr)
			}
			if e.records() != 0 {
				t.Fatal("a rejected answer stored records")
			}
		})
	}
}

// hubReplay stands for the Hub's idempotency: a key it has answered is replayed from its store without a new paid call.
type hubReplay struct {
	inner  LLMClient
	stored map[string]values.LLMResult
	paid   int
}

func (h *hubReplay) Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error) {
	if r, ok := h.stored[req.IdempotencyKey()]; ok {
		return r, nil
	}
	r, err := h.inner.Complete(ctx, req)
	if err == nil {
		h.stored[req.IdempotencyKey()] = r
		h.paid++
	}
	return r, err
}

// A rerun of the job (a crash, a retry of the runner) sends the same requests, so the Hub replays them instead of
// charging again (hard rule 7, AC-11): at most answerCalls paid calls per job.
func TestRepeatedCallsKeepTheirKeysAcrossRuns(t *testing.T) {
	e := newPPEnv(t)
	hub := &hubReplay{inner: e.llm, stored: map[string]values.LLMResult{}}
	d := e.deps()
	d.LLM = hub
	pp, err := NewPostProcessor(d)
	if err != nil {
		t.Fatal(err)
	}
	e.pp = pp
	e.llm.Answer(values.OperationSummary, `{"summary":"cut`)
	e.llm.Answer(values.OperationSummary, "prose")

	snap := e.snapshot("j1", SummarizeJobType, "t1")
	for run := range 3 {
		if err := e.handle(snap); !errors.Is(err, ErrTruncatedAnswer) && !errors.Is(err, ErrInvalidAnswer) {
			t.Fatalf("run %d: Handle = %v", run, err)
		}
		if hub.paid != answerCalls {
			t.Fatalf("run %d: paid calls = %d, want %d", run, hub.paid, answerCalls)
		}
	}

	// Another job (a manual retry) asks anew.
	if err := e.handle(e.snapshot("j2", SummarizeJobType, "t1")); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("Handle = %v", err)
	}
	if hub.paid != 2*answerCalls {
		t.Fatalf("paid calls = %d: a new job must not reuse the keys of the old one", hub.paid)
	}
}

// A network or quota error during a repeat keeps its classification: the job waits instead of failing.
func TestRepeatKeepsTheNetworkClassification(t *testing.T) {
	e := newPPEnv(t)
	e.llm.Reply(values.OperationSummary, testenv.LLMReply{Output: "prose"}, testenv.LLMReply{Err: shared.ErrNetworkUnavailable})
	err := e.handle(e.snapshot("j1", SummarizeJobType, "t1"))
	if !errors.Is(err, ErrLLM) || !errors.Is(err, shared.ErrNetworkUnavailable) || backoff.Classify(err) == backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want a network wait", err)
	}
	if st, ok := backoff.WaitStatus(err); !ok || st != job.StatusWaitingForNetwork {
		t.Fatalf("wait = %s %v", st, ok)
	}
}

// The log tells the shape of a rejected answer, never its text.
func TestRejectedAnswerLogsTheShapeOnly(t *testing.T) {
	const secret = "SECRET-LECTURE-TEXT"
	var logs bytes.Buffer
	e := newPPEnv(t)
	d := e.deps()
	d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	pp, err := NewPostProcessor(d)
	if err != nil {
		t.Fatal(err)
	}
	e.pp = pp
	e.llm.Answer(values.OperationSummary, "```json\n{\"summary\":\""+secret)

	if err := e.handle(e.snapshot("j1", SummarizeJobType, "t1")); !errors.Is(err, ErrTruncatedAnswer) {
		t.Fatalf("Handle = %v", err)
	}
	out := logs.String()
	if strings.Contains(out, secret) {
		t.Fatal("the log quotes the answer")
	}
	for _, want := range []string{"call=1", "call=3", "bytes=39", "start=fence", "truncated=true", "max_output"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}
