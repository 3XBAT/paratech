package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// fakeDeletion is the deletion use case: previews by id, the requests it got and the batch it answers.
type fakeDeletion struct {
	previews map[shared.RecordingID]recordingdelete.Preview
	errs     map[shared.RecordingID]error
	batch    recordingdelete.Batch
	batchErr error
	got      []recordingdelete.Request
}

func (f *fakeDeletion) Preview(_ context.Context, id shared.RecordingID) (recordingdelete.Preview, error) {
	if err := f.errs[id]; err != nil {
		return recordingdelete.Preview{}, err
	}
	p, ok := f.previews[id]
	if !ok {
		return recordingdelete.Preview{}, recordingdelete.ErrNotFound
	}
	return p, nil
}

func (f *fakeDeletion) DeleteMany(_ context.Context, reqs []recordingdelete.Request) (recordingdelete.Batch, error) {
	f.got = reqs
	return f.batch, f.batchErr
}

func newDeleteService(f *fakeDeletion) (*RecordingDeleteService, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	return NewRecordingDeleteService(f, slog.New(slog.NewTextHandler(logs, nil))), logs
}

func TestDeletePreviewView(t *testing.T) {
	started := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
	f := &fakeDeletion{previews: map[shared.RecordingID]recordingdelete.Preview{
		"r1": {Recording: "r1", Title: "Матанализ", StartedAt: started, Contents: deletion.Contents{
			Sound: true, Transcripts: 1, Summaries: 1, LectureTexts: 1, Jobs: 2, Assignments: []deletion.Assignment{
				{ID: "a1", Title: "Задача"}, {ID: "a2", Title: "Эссе", HasDraft: true}, {ID: "a3", Title: "Отчёт", Deliveries: 1},
			},
		}},
		"r2": {Recording: "r2", StartedAt: started, Refusal: recordingdelete.RefusalInProgress, Contents: deletion.Contents{Parts: 3}},
	}}
	s, _ := newDeleteService(f)

	got, err := s.Preview(context.Background(), []string{"r1", "gone", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	want := DeletePreview{
		Recordings: []DeleteRecording{
			{ID: "r1", Title: "Матанализ", StartedAt: "2026-10-19T09:30:00Z", Deletable: true, Sound: true, Transcript: true,
				Summary: true, LectureText: true, Jobs: 2,
				Deletes: []string{"Звук записи", "Расшифровка", "Краткое содержание", "Текст лекции", "Незавершённые задачи обработки: 2"},
				Assignments: []DeleteAssignment{
					{ID: "a1", Title: "Задача", Class: "plain"}, {ID: "a2", Title: "Эссе", Class: "draft"},
					{ID: "a3", Title: "Отчёт", Class: "emails"},
				}},
			{ID: "r2", StartedAt: "2026-10-19T09:30:00Z", Refusal: "in_progress", RefusalMessage: msgRefusalInProgress,
				Transcript: true, Deletes: []string{"Расшифровка"}, Assignments: []DeleteAssignment{}},
		},
		Missing: []string{"gone"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Preview =\n %+v\nwant\n %+v", got, want)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"recordings", "missing", "id", "title", "startedAt", "deletable", "refusal", "refusalMessage",
		"sound", "transcript", "summary", "lectureText", "jobs", "deletes", "assignments", "class"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("preview JSON has no %q", k)
		}
	}
}

func TestDeletePreviewErrors(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		err  error
		want error
	}{
		{"bad id", []string{"a/b"}, nil, ErrDeleteInvalidID},
		{"too many", make([]string, recordingdelete.MaxBatch+1), nil, ErrDeleteTooMany},
		{"storage", []string{"r1"}, fmt.Errorf("%w: C:\\Users\\x\\db locked", recordingdelete.ErrStorage), ErrDeleteUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, logs := newDeleteService(&fakeDeletion{errs: map[shared.RecordingID]error{"r1": tc.err}})
			_, err := s.Preview(context.Background(), tc.ids)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Preview = %v, want %v", err, tc.want)
			}
			if tc.err != nil && !strings.Contains(logs.String(), "db locked") {
				t.Fatalf("the cause is not logged: %s", logs)
			}
		})
	}
}

func TestDeleteResult(t *testing.T) {
	f := &fakeDeletion{batch: recordingdelete.Batch{
		Results: []recordingdelete.Result{
			{Recording: "r1", Outcome: recordingdelete.OutcomeDeleted, Kept: []shared.AssignmentID{"a3"}},
			{Recording: "r2", Outcome: recordingdelete.OutcomeDeleted},
		},
		Failures: []recordingdelete.Failure{
			{Recording: "r3", Err: recordingdelete.ErrInProgress},
			{Recording: "r4", Err: recordingdelete.ErrRecovering},
			{Recording: "r5", Err: fmt.Errorf("%w: %w", recordingdelete.ErrInvalidRequest, deletion.ErrUnknownAssignment)},
			{Recording: "r6", Err: recordingdelete.ErrNotFound},
			{Recording: "r7", Err: recordingdelete.ErrStorage},
		},
	}}
	s, _ := newDeleteService(f)
	got, err := s.Delete(context.Background(), []DeleteRequest{
		{RecordingID: "r1", AssignmentIDs: []string{"a1", "a2"}}, {RecordingID: "r2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := DeleteResult{
		Removed: 2, Deleted: 2, Message: "Удалено 2", KeptAssignments: []string{"a3"},
		Failures: []DeleteFailure{
			{RecordingID: "r3", Message: ErrDeleteInProgress.Error()}, {RecordingID: "r4", Message: ErrDeleteRecovering.Error()},
			{RecordingID: "r5", Message: ErrDeleteChoice.Error()}, {RecordingID: "r6", Message: ErrDeleteNotFound.Error()},
			{RecordingID: "r7", Message: ErrDeleteUnavailable.Error()},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Delete =\n %+v\nwant\n %+v", got, want)
	}
	wantReqs := []recordingdelete.Request{
		{Recording: "r1", Assignments: []shared.AssignmentID{"a1", "a2"}}, {Recording: "r2"},
	}
	if !reflect.DeepEqual(f.got, wantReqs) {
		t.Fatalf("requests = %+v", f.got)
	}

	f.batch = recordingdelete.Batch{Results: []recordingdelete.Result{
		{Recording: "r1", Outcome: recordingdelete.OutcomeDeleted}, {Recording: "r2", Outcome: recordingdelete.OutcomePending},
	}}
	got, err = s.Delete(context.Background(), []DeleteRequest{{RecordingID: "r1"}, {RecordingID: "r2"}})
	if err != nil || got.Removed != 2 || got.Pending != 1 || got.Message != "Удалено 2. Часть файлов удалится при следующем запуске." {
		t.Fatalf("Delete with a pending one = %+v, %v", got, err)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"removed", "deleted", "pending", "message", "failures", "keptAssignments"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("result JSON has no %q", k)
		}
	}
}

func TestDeleteRefusesBadRequests(t *testing.T) {
	cases := []struct {
		name string
		reqs []DeleteRequest
		want error
	}{
		{"bad recording id", []DeleteRequest{{RecordingID: "a\\b"}}, ErrDeleteInvalidID},
		{"bad assignment id", []DeleteRequest{{RecordingID: "r1", AssignmentIDs: []string{"x y"}}}, ErrDeleteChoice},
		{"too many", make([]DeleteRequest, recordingdelete.MaxBatch+1), ErrDeleteTooMany},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDeletion{}
			s, _ := newDeleteService(f)
			if _, err := s.Delete(context.Background(), tc.reqs); !errors.Is(err, tc.want) {
				t.Fatalf("Delete = %v, want %v", err, tc.want)
			}
			if f.got != nil {
				t.Fatal("a refused request reached the use case")
			}
		})
	}

	f := &fakeDeletion{batchErr: recordingdelete.ErrInvalidRequest}
	s, _ := newDeleteService(f)
	if _, err := s.Delete(context.Background(), []DeleteRequest{{RecordingID: "r1"}}); !errors.Is(err, ErrDeleteChoice) {
		t.Fatalf("Delete with a refused batch = %v, want ErrDeleteChoice", err)
	}
}

func TestDeleteServiceMethodSet(t *testing.T) {
	// Every exported method is callable from the page; the frontend wrapper calls exactly these.
	typ := reflect.TypeFor[*RecordingDeleteService]()
	var names []string
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	if !reflect.DeepEqual(names, []string{"Delete", "Preview"}) {
		t.Fatalf("methods = %v", names)
	}
}
