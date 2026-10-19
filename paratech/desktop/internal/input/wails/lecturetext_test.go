package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
)

func lectureBlock(t *testing.T, heading string, startMs int64, paragraphs ...string) lecturetext.Block {
	t.Helper()
	b, err := lecturetext.NewBlock(heading, startMs, paragraphs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type lectureFixture struct {
	svc      *LectureTextService
	uc       *fakeSummary
	clip     *fakeClipboard
	picked   []string
	pickPath string
	pickErr  error
	logs     *bytes.Buffer
}

func newLectureFixture() *lectureFixture {
	f := &lectureFixture{uc: &fakeSummary{}, clip: &fakeClipboard{}, logs: &bytes.Buffer{}}
	picker := summaryPickerFunc(func(_ context.Context, name, format string) (string, error) {
		f.picked = append(f.picked, name+" "+format)
		return f.pickPath, f.pickErr
	})
	f.svc = NewLectureTextService(f.uc, picker, f.clip, slog.New(slog.NewTextHandler(f.logs, nil)))
	return f
}

// The lecture text of the summary page (TASK-090): its state with the part progress, and what may start it.
func TestSummaryViewLectureTextStates(t *testing.T) {
	tests := []struct {
		name        string
		lecture     summaryview.LectureText
		transcribed bool
		want        SummaryLectureText
	}{
		{
			name: "before the transcription", lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateNone}},
			want: SummaryLectureText{SummaryStep: SummaryStep{State: "none", Message: msgLectureNone}},
		},
		{
			name: "never made", transcribed: true,
			lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateNone}, CanStart: true},
			want: SummaryLectureText{
				SummaryStep: SummaryStep{State: "none", Message: msgLectureMissing}, CanStart: true, RetryWork: "lectureText",
			},
		},
		{
			name: "running with parts", transcribed: true,
			lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateRunning}, PartsDone: 3, Parts: 12},
			want: SummaryLectureText{
				SummaryStep: SummaryStep{State: "running", Message: "Текст лекции составляется: готово частей 3 из 12."},
				PartsDone:   3, Parts: 12,
			},
		},
		{
			name: "running before the first part", transcribed: true,
			lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateRunning}},
			want:    SummaryLectureText{SummaryStep: SummaryStep{State: "running", Message: msgLectureRunning}},
		},
		{
			name: "waits for the network with parts", transcribed: true,
			lecture: summaryview.LectureText{
				Step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitNetwork}, PartsDone: 4, Parts: 12,
			},
			want: SummaryLectureText{
				SummaryStep: SummaryStep{
					State: "waiting", WaitReason: "network", Message: "Текст лекции: готово частей 4 из 12. " + msgWaitNetwork,
				},
				PartsDone: 4, Parts: 12,
			},
		},
		{
			name: "waits in line", transcribed: true,
			lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitInLine}},
			want: SummaryLectureText{
				SummaryStep: SummaryStep{State: "waiting", WaitReason: "inLine", Message: msgLectureWaiting},
			},
		},
		{
			name: "failed", transcribed: true,
			lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateFailed, JobError: "x"}},
			want: SummaryLectureText{
				SummaryStep: SummaryStep{State: "failed", Message: msgLectureFailed, JobError: "x"}, RetryWork: "lectureText",
			},
		},
		{
			name: "done", transcribed: true, lecture: summaryview.LectureText{Step: summaryview.Step{State: summaryview.StateDone}},
			want: SummaryLectureText{SummaryStep: SummaryStep{State: "done", Message: msgLectureDone}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSummaryFixture()
			f.uc.view = summaryview.View{Recording: "r1", Transcribed: tc.transcribed, LectureText: tc.lecture}
			got, err := f.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			tc.want.Blocks = []SummaryLectureBlock{}
			if !reflect.DeepEqual(got.LectureText, tc.want) {
				t.Fatalf("lecture text = %+v\nwant           %+v", got.LectureText, tc.want)
			}
		})
	}
}

// The page reads these names; blocks are never null.
func TestSummaryLectureTextJSON(t *testing.T) {
	f := newSummaryFixture()
	f.uc.view = summaryview.View{Recording: "r1"}
	got, err := f.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got.LectureText)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"state":"","waitReason":"","message":"` + msgLectureNone + `","jobError":"","ready":false,"blocks":[],` +
		`"updatedAt":"","partsDone":0,"parts":0,"canStart":false,"retryWork":""}`
	if string(data) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", data, want)
	}
	block, _ := json.Marshal(SummaryLectureBlock{Heading: "h", StartSeconds: 65, Time: "01:05", Paragraphs: []string{"p"}})
	if string(block) != `{"heading":"h","startSeconds":65,"time":"01:05","paragraphs":["p"]}` {
		t.Fatalf("block JSON = %s", block)
	}
}

func TestLectureTextCopy(t *testing.T) {
	f := newLectureFixture()
	f.uc.text = "Введение\n\nТекст лекции"
	if err := f.svc.Copy(context.Background(), "r1", true); err != nil || f.clip.text != f.uc.text || !f.uc.lectureTimes {
		t.Fatalf("Copy = %v, clipboard %q, times %v", err, f.clip.text, f.uc.lectureTimes)
	}

	f.clip.err = errors.New("clipboard busy")
	if err := f.svc.Copy(context.Background(), "r1", false); !errors.Is(err, ErrSummaryClipboard) {
		t.Fatalf("clipboard failure: %v", err)
	}
	if strings.Contains(f.logs.String(), "Текст лекции") {
		t.Fatal("the lecture text reached the log")
	}

	tests := []struct {
		name string
		id   string
		err  error
		want error
	}{
		{"invalid id", "a/b", nil, ErrSummaryInvalidID},
		{"no lecture text", "r1", summaryview.ErrNoLectureText, ErrLectureTextEmpty},
		{"recording gone", "r1", summaryview.ErrRecordingNotFound, ErrSummaryNotFound},
		{"storage", "r1", errors.Join(summaryview.ErrUnavailable, errors.New(`C:\Users\student\db locked`)), ErrLectureTextUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLectureFixture()
			f.uc.err = tc.err
			if err := f.svc.Copy(context.Background(), tc.id, false); !errors.Is(err, tc.want) {
				t.Fatalf("Copy = %v, want %v", err, tc.want)
			}
			if strings.Contains(f.logs.String(), "student") && tc.want != ErrLectureTextUnavailable {
				t.Fatal("unexpected log")
			}
		})
	}
}

func TestLectureTextSaveAs(t *testing.T) {
	f := newLectureFixture()
	f.uc.name = "География — текст лекции 2026-10-19.md"
	f.pickPath = `C:\Users\student\Документы\География — текст лекции 2026-10-19.md`

	got, err := f.svc.SaveAs(context.Background(), "r1", "md", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != (SummarySaveResult{Status: "saved", Message: msgLectureSaved, Name: "География — текст лекции 2026-10-19.md"}) {
		t.Fatalf("SaveAs = %+v", got)
	}
	if !reflect.DeepEqual(f.picked, []string{"География — текст лекции 2026-10-19.md md"}) {
		t.Fatalf("picker got %v", f.picked)
	}
	if e := f.uc.exported; e.id != "r1" || e.f != summaryexport.FormatMarkdown || e.path != f.pickPath || !f.uc.lectureTimes {
		t.Fatalf("exported %+v, times %v", e, f.uc.lectureTimes)
	}

	tests := []struct {
		name      string
		format    string
		pickPath  string
		pickErr   error
		ucErr     error
		exportErr error
		want      SummarySaveResult
		wantErr   error
	}{
		{name: "cancelled", format: "txt", want: SummarySaveResult{Status: "cancelled", Message: msgSummaryCancelled}},
		{name: "dialog failed", format: "txt", pickErr: errors.New("dialog"), want: SummarySaveResult{Status: "failed", Message: msgSummaryFailed}},
		{name: "write failed", format: "txt", pickPath: `C:\student\x.txt`,
			exportErr: errors.Join(summaryview.ErrUnavailable, errors.New("access denied")),
			want:      SummarySaveResult{Status: "failed", Message: msgSummaryFailed}},
		{name: "no lecture text", format: "txt", ucErr: summaryview.ErrNoLectureText, wantErr: ErrLectureTextEmpty},
		{name: "gone meanwhile", format: "txt", pickPath: `C:\student\x.txt`, exportErr: summaryview.ErrNoLectureText,
			wantErr: ErrLectureTextEmpty},
		{name: "unknown format", format: "html", wantErr: ErrSummaryFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLectureFixture()
			f.pickPath, f.pickErr, f.uc.err, f.uc.exportErr = tc.pickPath, tc.pickErr, tc.ucErr, tc.exportErr
			got, err := f.svc.SaveAs(context.Background(), "r1", tc.format, false)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SaveAs = %+v, %v; want %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("SaveAs = %+v, %v; want %+v", got, err, tc.want)
			}
			if strings.Contains(f.logs.String(), "student") {
				t.Fatal("the path reached the log")
			}
		})
	}
}
