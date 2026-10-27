package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// pickedFile is the open dialog in the test: it always picks path.
type pickedFile string

func (p pickedFile) PickRecordingFile(context.Context) (string, error) { return string(p), nil }

// oldLectureAudio is a synthetic WAV of an old lecture: a valid header (16 kHz mono 16-bit, 2 ms) and binary data, so
// a text-only path could not carry it unchanged.
func oldLectureAudio() []byte {
	data := []byte("\x00\xff\x80\x7f old lecture recorded elsewhere \xfe\x01\x00\xff\x10\x20\x30\x40\x50\x60\x70\x80\x90")
	data = append(data, make([]byte, 64-len(data))...)
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(36+len(data)))...)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, 16000)
	b = binary.LittleEndian.AppendUint32(b, 32000)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

// TASK-161 on the wired core: an old recording picked in the dialog is copied, transcribed by the pinned model and
// post-processed; the Hub never sees the audio (AC-06, hard rule 2). The same file again is the same recording, and
// deleting it (ADR-0018) removes the copy only: the student's original stays as it was.
func TestImportOldRecordingThroughTheWiredCore(t *testing.T) {
	ctx := context.Background()
	e, _, hub, hubURL := pipelineEnv(t)
	u := openUICore(t, t.TempDir(), e)
	configureHub(t, u.c, time.Now(), hubURL)
	model := importModel(t, u.c, "base", testenv.WhisperOK)
	if err := u.c.models.SetDefault(ctx, model); err != nil {
		t.Fatal(err)
	}
	u.start(t)

	// The student's original, outside the data directory.
	audio := oldLectureAudio()
	original := filepath.Join(t.TempDir(), "Лекция 3.wav")
	if err := os.WriteFile(original, audio, 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(original, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	svc := wails.NewRecordingImportService(u.c.ui.RecordingImport, pickedFile(original), log)
	lecture := wails.RecordingImportLecture{
		StartedAt: time.Now().Add(-48 * time.Hour).Format(time.RFC3339), Subject: "English", Kind: "lecture",
	}
	res, err := svc.Import(ctx, lecture)
	if err != nil || res.Existing || res.Format != "wav" || res.Name != "Лекция 3.wav" {
		t.Fatalf("Import = %+v, %v", res, err)
	}
	id := shared.RecordingID(res.RecordingID)
	rec, err := sqlite.NewRecordingRepository(u.c.db).Get(ctx, id)
	if err != nil || rec.Origin() != recording.OriginImported || rec.Status() != recording.StatusCompleted {
		t.Fatalf("stored recording %+v, %v", rec, err)
	}
	copied, err := u.c.store.Open(ctx, rec.Audio())
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	_, err = got.ReadFrom(copied)
	_ = copied.Close()
	if err != nil || !bytes.Equal(got.Bytes(), audio) {
		t.Fatalf("the copy differs from the original: %v", err)
	}

	// The usual STT job, pinned to the default model, then the post-processing on the Hub.
	jobs := sqlite.NewJobRepository(u.c.db)
	stt := onlyJob(t, jobs, transcribe.JobType)
	if want := fmt.Sprintf("stt:%s:%s", id, model); stt.Key() != want || stt.PinnedModelID() != model {
		t.Fatalf("STT job key %q pin %q, want %q", stt.Key(), stt.PinnedModelID(), want)
	}
	if chain := statusChain(t, jobs, stt.ID(), job.StatusCompleted); !orderedChain(chain) {
		t.Fatalf("STT job went %v", chain)
	}
	for _, typ := range []job.Type{homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType, homeworkflow.LectureTextJobType} {
		if chain := statusChain(t, jobs, onlyJob(t, jobs, typ).ID(), job.StatusCompleted); !orderedChain(chain) {
			t.Fatalf("%s job went %v", typ, chain)
		}
	}
	reqs := hub.Requests()
	if len(reqs) == 0 {
		t.Fatal("the Hub got no post-processing request")
	}
	for i, r := range reqs {
		raw, err := proto.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range audioForms(audio) {
			if bytes.Contains(raw, form) {
				t.Fatalf("Hub request %d (%s) carries the imported audio as %q", i, r.GetSpec().GetOperationType(), form)
			}
		}
	}

	// The same bytes again: the same recording, no second job.
	again, err := svc.Import(ctx, wails.RecordingImportLecture{StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)})
	if err != nil || !again.Existing || again.RecordingID != res.RecordingID {
		t.Fatalf("second Import = %+v, %v; want the existing recording", again, err)
	}
	onlyJob(t, jobs, transcribe.JobType)

	// «Удалить»: the recording and its copy go, the original stays byte for byte with its time.
	deletion := wails.NewRecordingDeleteService(u.c.ui.Deletion, log)
	if out, err := deletion.Delete(ctx, []wails.DeleteRequest{{RecordingID: res.RecordingID}}); err != nil || out.Deleted != 1 {
		t.Fatalf("Delete = %+v, %v", out, err)
	}
	checkGone(t, u.c, id, []shared.ArtifactRef{rec.Audio()})
	kept, err := os.ReadFile(original)
	if err != nil || !bytes.Equal(kept, audio) {
		t.Fatalf("the original after the deletion: %v, same bytes %v", err, bytes.Equal(kept, audio))
	}
	if fi, err := os.Stat(original); err != nil || !fi.ModTime().Equal(stamp) {
		t.Fatalf("the original was touched: %v", err)
	}

	// After the deletion the same file is a new recording again.
	third, err := svc.Import(ctx, lecture)
	if err != nil || third.Existing || third.RecordingID == res.RecordingID {
		t.Fatalf("Import after the deletion = %+v, %v; want a new recording", third, err)
	}
}

// A file whose content is not audio is refused with the page's text, whatever its name; nothing is stored.
func TestImportRefusesNonAudioThroughTheWiredCore(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	u := openUICore(t, t.TempDir(), e)
	picked := filepath.Join(t.TempDir(), "lecture.mp3")
	if err := os.WriteFile(picked, []byte("PK\x03\x04 a zip, not a lecture"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := wails.NewRecordingImportService(u.c.ui.RecordingImport, pickedFile(picked), slog.New(slog.DiscardHandler))
	_, err := svc.Import(ctx, wails.RecordingImportLecture{StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)})
	if !errors.Is(err, wails.ErrRecordingImportFormat) {
		t.Fatalf("Import = %v, want the format refusal", err)
	}
	if recs, err := u.c.ui.Activity.ListRecordings(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("recordings after a refusal: %+v, %v", recs, err)
	}
}
