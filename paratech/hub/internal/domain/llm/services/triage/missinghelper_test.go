package triage_test

import (
	"errors"
	"strings"
	"testing"

	"paratech/hub/internal/domain/llm/services/triage"
)

// TestMissingHelperIsItsOwnRefusal: only a partial or impossible work without a helper is ErrMissingHelper (the defect
// the Hub asks once more about, TASK-200); every other refusal is not, and all are ErrInvalidAnswer.
func TestMissingHelperIsItsOwnRefusal(t *testing.T) {
	in := mustInput(t, round1)
	helper := partialAnswer[strings.Index(partialAnswer, `"helper":`):]
	for name, tc := range map[string]struct {
		out     string
		missing bool
	}{
		"partial without helper": {strings.Replace(partialAnswer, helper, `"helper":null}`, 1), true},
		"impossible no helper": {strings.Replace(impossibleAnswer, impossibleAnswer[strings.Index(impossibleAnswer, `"helper":`):],
			`"helper":null}`, 1), true},
		"full with helper":  {strings.Replace(fullAnswer, `"helper":null}`, helper, 1), false},
		"partial no reason": {strings.Replace(partialAnswer, `"Измерения делает студент в лаборатории."`, `""`, 1), false},
		"empty helper":      {strings.Replace(partialAnswer, helper, `"helper":{}}`, 1), false},
		"helper left out":   {strings.Replace(partialAnswer, ","+helper, "}", 1), false},
	} {
		_, err := in.ParseAnswer(tc.out)
		if !errors.Is(err, triage.ErrInvalidAnswer) || errors.Is(err, triage.ErrMissingHelper) != tc.missing {
			t.Errorf("%s: err = %v, want ErrMissingHelper %t", name, err, tc.missing)
		}
	}
	if strings.Contains(triage.MissingHelperNote, "Измерения") || !strings.Contains(triage.MissingHelperNote, `"helper"`) {
		t.Errorf("the note = %q", triage.MissingHelperNote)
	}
}
