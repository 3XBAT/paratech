package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// completedAt is a completed recording id that started at recT0 plus offset; silent makes its track silent.
func completedAt(t *testing.T, id string, offset time.Duration, silent bool) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), MaxDuration: 3 * time.Hour}, recT0.Add(offset))
	if err != nil {
		t.Fatal(err)
	}
	at := recT0.Add(offset + time.Second)
	track := values.AssembledTrack{Duration: time.Hour, Seconds: 3600, AudibleSeconds: 3600}
	if silent {
		track.AudibleSeconds = 1
	}
	for _, step := range []func() error{
		func() error { return r.Begin(at) },
		func() error { return r.Stop(at.Add(time.Hour)) },
		func() error { return r.Finalize(at.Add(time.Hour)) },
		func() error { return r.CompleteTrack(at.Add(time.Hour), recRef(t, "recordings/"+id+".wav"), track) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func saveKBTranscript(t *testing.T, db *DB, id string, rec shared.RecordingID, at time.Time) {
	t.Helper()
	text, err := transcript.DocumentRef(shared.TranscriptID(id))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transcript.New(transcript.Spec{
		ID: shared.TranscriptID(id), RecordingID: rec, ModelID: "m1", ModelName: "base", ModelVersion: "1", Text: text,
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewTranscriptRepository(db).Save(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeBaseListsLessonsAndReadiness(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	recs := NewRecordingRepository(db)
	saveModel(t, NewModelRepository(db), newModel(t, "m1", "base", "1"))
	if err := NewLessonRepository(db).Save(ctx, newTestLesson(t, "l1", "", 0)); err != nil {
		t.Fatal(err)
	}

	older, newer := completedAt(t, "older", 0, false), completedAt(t, "newer", time.Hour, false)
	bare, silent := completedAt(t, "bare", 30*time.Minute, false), completedAt(t, "silent", 2*time.Hour, true)
	deleting := completedAt(t, "deleting", 3*time.Hour, false)
	if err := deleting.MarkDeleting(recT0.Add(4*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*recording.Recording{older, newer, bare, silent, deleting, recordingInStatus(t, recording.StatusFailed)} {
		saveRecording(t, recs, r)
	}

	// newer: an old transcript with everything, a new one with a summary and an unfinished lecture text.
	saveKBTranscript(t, db, "t-old", "newer", recT0.Add(3*time.Hour))
	saveKBTranscript(t, db, "t-new", "newer", recT0.Add(4*time.Hour))
	saveKBTranscript(t, db, "t-older", "older", recT0.Add(3*time.Hour))
	summaries, lectures := NewSummaryRepository(db), NewLectureTextRepository(db)
	for _, id := range []shared.TranscriptID{"t-old", "t-new"} {
		if err := summaries.Save(ctx, newTestSummary(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	published := newTestLectureText(t, "t-older")
	blocks := []lecturetext.Block{lectureBlock(t, "Реки", 0, "О реках.")}
	if err := published.AddPart("run", 1, 0, blocks, asgT0); err != nil {
		t.Fatal(err)
	}
	if err := published.Publish("run", blocks, 1, asgT0); err != nil {
		t.Fatal(err)
	}
	for _, l := range []*lecturetext.LectureText{published, newTestLectureText(t, "t-new"), newTestLectureText(t, "t-old")} {
		if err := lectures.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	}

	kb := NewKnowledgeBase(db)
	list, unreadable, err := kb.ListLessons(ctx)
	if err != nil || len(unreadable) != 0 {
		t.Fatalf("ListLessons: %v, unreadable %v", err, unreadable)
	}
	var ids []shared.RecordingID
	for _, r := range list {
		ids = append(ids, r.ID())
	}
	if want := []shared.RecordingID{"newer", "bare", "older"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ListLessons = %v, want %v (silent, deleting and failed left out)", ids, want)
	}

	ready, err := kb.ListReadiness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[shared.RecordingID]knowledgebase.Readiness{
		"newer": {Transcript: "t-new", Summary: true},
		"older": {Transcript: "t-older", LectureText: true},
		"bare":  {},
	}
	if !reflect.DeepEqual(ready, want) {
		t.Fatalf("ListReadiness = %+v, want %+v", ready, want)
	}

	// A row the aggregate cannot restore is reported, not listed, and does not hide the others.
	mustExec(t, db, `UPDATE recordings SET chunks_ref = 'secrets/bad' WHERE id = 'bare'`)
	list, unreadable, err = kb.ListLessons(ctx)
	if err != nil || len(list) != 2 || !reflect.DeepEqual(unreadable, []shared.RecordingID{"bare"}) {
		t.Fatalf("ListLessons with a corrupt row = %d, %v, %v", len(list), unreadable, err)
	}
}

func TestRecordingLessonCorrectionRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewRecordingRepository(db)
	r := completedAt(t, "r1", 0, false)
	saveRecording(t, repo, r)

	c, err := values.NewLessonCorrection("Физика", "lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CorrectLesson(recT0.Add(3*time.Hour), c); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, r)
	got, err := repo.Get(ctx, "r1")
	if err != nil || got.LessonCorrection() != c || !reflect.DeepEqual(got, r) {
		t.Fatalf("Get = %+v, %v; want the correction %+v", got, err, c)
	}

	if _, err := got.CorrectLesson(recT0.Add(4*time.Hour), values.LessonCorrection{}); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, got)
	if n := scanInt(t, db.sql, "SELECT count(*) FROM recordings WHERE user_subject IS NULL AND user_kind IS NULL"); n != 1 {
		t.Fatalf("a cleared correction is stored as %d NULL rows, want NULL", n)
	}

	// The schema refuses a kind the domain does not know.
	if _, err := db.sql.ExecContext(ctx, "UPDATE recordings SET user_kind = 'party'"); err == nil {
		t.Fatal("an unknown kind was stored")
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE recordings SET user_subject = ''"); err == nil {
		t.Fatal("an empty subject was stored")
	}
}

// A stale save of a recording does not undo a correction: the version guard refuses it.
func TestRecordingCorrectionVersionGuarded(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewRecordingRepository(db)
	saveRecording(t, repo, completedAt(t, "r1", 0, false))
	a, err := repo.Get(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Get(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := values.NewLessonCorrection("Физика", "")
	if _, err := a.CorrectLesson(recT0.Add(3*time.Hour), c); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, a)
	if _, err := b.CorrectLesson(recT0.Add(3*time.Hour), values.LessonCorrection{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, b); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
	}
}
