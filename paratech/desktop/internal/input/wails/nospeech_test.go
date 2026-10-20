package wails

import (
	"context"
	"testing"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/recording/recording"
)

// TASK-110: a recording without speech reaches the page as errorCode "noSpeech" with the sound check and no retry.
func TestActivityServiceListsNoSpeech(t *testing.T) {
	uc := &fakeActivity{recs: []activity.Recording{{ID: "r1", Status: recording.StatusCompleted, Chain: activity.Chain{
		Stage: activity.StageFailed, Step: activity.StepLectureText, ErrorCode: activity.ErrorNoSpeech,
		Action: activity.ActionCheckSound,
	}}}}
	got, err := NewActivityService(uc, &fakeStopper{}, &fakeForcer{}, &fakeRetrier{}, discard()).ListRecordings(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("ListRecordings = %+v, %v", got, err)
	}
	if r := got[0]; r.Stage != "failed" || r.ErrorCode != "noSpeech" || r.Action != "checkSound" || r.RetryWork != "" ||
		r.JobError != "" {
		t.Fatalf("row %+v", r)
	}
}

// TASK-110: the summary page of a recording without speech says so in every step, without «Повторить».
func TestSummaryViewNoSpeech(t *testing.T) {
	f := newSummaryFixture()
	none := summaryview.Step{State: summaryview.StateNoSpeech}
	f.uc.view = summaryview.View{
		Recording: "r1", Transcribed: true, NoSpeech: true, Summary: summaryview.Summary{Step: none},
		Homework: summaryview.Homework{Step: none}, LectureText: summaryview.LectureText{Step: none},
	}
	got, err := f.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoSpeech || got.LectureText.RetryWork != "" || got.LectureText.CanStart {
		t.Fatalf("view %+v", got)
	}
	for _, st := range []SummaryStep{got.Summary.SummaryStep, got.Homework.SummaryStep, got.LectureText.SummaryStep} {
		if st.State != "noSpeech" || st.Message != msgNoSpeech || st.JobError != "" {
			t.Fatalf("step %+v", st)
		}
	}
}
