package homeworkflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// TASK-110: a transcript stored before the speech check, with only whisper's marks (the owner's 473 "[BLANK_AUDIO]"
// segments), fails every post-processing job at once with the fixed nothing-to-process error and never calls the Hub,
// also on «Повторить» (a new job of the same transcript).
func TestNoSpeechNeverCallsTheHub(t *testing.T) {
	blank := make([]string, 473)
	for i := range blank {
		blank[i] = " [BLANK_AUDIO]"
	}
	transcripts := map[string][]string{
		"blank audio": blank,
		"marks":       {"[ Silence ]", "(музыка)", "[MUSIC]", "♪♪", "*music*", " ..."},
	}
	for name, lines := range transcripts {
		for _, typ := range []job.Type{SummarizeJobType, ExtractHomeworkJobType, LectureTextJobType} {
			t.Run(name+"/"+string(typ), func(t *testing.T) {
				e := newPPEnv(t)
				e.addTranscript("r3", "l1", "t3", lessonStart, lines...)
				for _, id := range []shared.JobID{"j1", "j2"} {
					err := e.handle(e.snapshot(id, typ, "t3"))
					if !errors.Is(err, ErrNothingToProcess) || backoff.Classify(err) != backoff.ClassPermanent {
						t.Fatalf("%s: Handle = %v, want a permanent ErrNothingToProcess", id, err)
					}
					// The recordings screen recognises the job's last error by this suffix.
					if !strings.HasSuffix(err.Error(), values.NothingToProcessMessage) {
						t.Fatalf("%s: message %q", id, err.Error())
					}
				}
				if n := len(e.llm.Requests()); n != 0 {
					t.Fatalf("LLM requests = %d, want 0", n)
				}
				if e.records() != 0 {
					t.Fatalf("%d records stored", e.records())
				}
			})
		}
	}
}

// TASK-110: whisper's marks among speech never reach the model; the speech does.
func TestMarksDoNotReachTheHub(t *testing.T) {
	tests := []struct {
		op     values.Operation
		typ    job.Type
		answer string
	}{
		{values.OperationSummary, SummarizeJobType, summaryAnswer},
		{values.OperationExtractHomework, ExtractHomeworkJobType, extractAnswer},
	}
	for _, tc := range tests {
		t.Run(string(tc.typ), func(t *testing.T) {
			e := newPPEnv(t)
			e.addTranscript("r3", "l1", "t3", lessonStart,
				" [BLANK_AUDIO]", " Сегодня решаем (x + 1) в квадрате.", "(музыка)", " [MUSIC] Homework: chapter 4.", "[_TT_150]")
			e.llm.Answer(tc.op, tc.answer)
			if err := e.handle(e.snapshot("j1", tc.typ, "t3")); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			reqs := e.llm.Requests()
			if len(reqs) != 1 {
				t.Fatalf("LLM requests = %d, want 1", len(reqs))
			}
			data := reqs[0].UntrustedData()
			for _, mark := range []string{"BLANK_AUDIO", "MUSIC", "музыка", "_TT_"} {
				if strings.Contains(data, mark) {
					t.Errorf("the request holds the mark %q: %s", mark, data)
				}
			}
			for _, speech := range []string{"Сегодня решаем (x + 1) в квадрате.", "Homework: chapter 4."} {
				if !strings.Contains(data, speech) {
					t.Errorf("the request lacks %q: %s", speech, data)
				}
			}
		})
	}
}

// TASK-110 review: only a transcript without speech is the no-speech error; a missing lesson field and a refused
// context keep errors that offer «Повторить».
func TestContextError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"missing lesson start", fmt.Errorf("%w: lesson start", contextselection.ErrMissingContext), ErrIncompleteContext},
		{"missing transcript", fmt.Errorf("%w: transcript is empty", contextselection.ErrMissingContext), ErrIncompleteContext},
		{"invalid material", contextselection.ErrInvalidMaterial, ErrContextRefused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := contextError(tc.err)
			if got != tc.want || values.IsNothingToProcess(got.Error()) {
				t.Fatalf("contextError = %v, want %v", got, tc.want)
			}
		})
	}
}
