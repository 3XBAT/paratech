package values

import (
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestPostProcessJobKeyPrefix(t *testing.T) {
	tests := []struct {
		typ        string
		transcript shared.TranscriptID
		want       string
	}{
		{SummarizeJobType, "t1", "summarize:t1:"},
		{ExtractHomeworkJobType, "stt-1", "extract_homework:stt-1:"},
	}
	for _, tc := range tests {
		if got := PostProcessJobKeyPrefix(tc.typ, tc.transcript); got != tc.want {
			t.Errorf("PostProcessJobKeyPrefix(%s, %s) = %q, want %q", tc.typ, tc.transcript, got, tc.want)
		}
	}
}
