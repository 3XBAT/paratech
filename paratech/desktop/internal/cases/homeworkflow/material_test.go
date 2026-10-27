package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// ppMaterials is the library as the material jobs read it: materials with their texts, or an error per material.
type ppMaterials struct {
	items map[shared.MaterialID]*material.Material
	texts map[shared.MaterialID]material.Text
	errs  map[shared.MaterialID]error
}

func (f *ppMaterials) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	m, ok := f.items[id]
	if !ok {
		return nil, fmt.Errorf("material %s: %w", id, shared.ErrNotFound)
	}
	return m, nil
}

func (f *ppMaterials) MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	if err := f.errs[id]; err != nil {
		return nil, material.Text{}, err
	}
	m, err := f.Material(ctx, id)
	if err != nil {
		return nil, material.Text{}, err
	}
	return m, f.texts[id], nil
}

var (
	_ MaterialTexts  = (*ppMaterials)(nil)
	_ MaterialLookup = (*ppMaterials)(nil)
)

// materialEnv is a ppEnv whose PostProcessor also runs the material jobs.
type materialEnv struct {
	*ppEnv
	lib *ppMaterials
}

func newMaterialEnv(t *testing.T) *materialEnv {
	t.Helper()
	e := &materialEnv{ppEnv: newPPEnv(t), lib: &ppMaterials{
		items: map[shared.MaterialID]*material.Material{}, texts: map[shared.MaterialID]material.Text{},
		errs: map[shared.MaterialID]error{},
	}}
	d := e.deps()
	d.Materials, d.MaterialAssignments = e.lib, ppAssignments{e.ppStore}
	pp, err := NewPostProcessor(d)
	if err != nil {
		t.Fatal(err)
	}
	e.pp = pp
	return e
}

// addMaterial stores a material of type typ with the fragments of text, linked to lessons.
func (e *materialEnv) addMaterial(id shared.MaterialID, typ material.Type, fs []material.Fragment, lessons ...shared.LessonID) *material.Material {
	e.t.Helper()
	m, err := material.New(material.Spec{
		ID: id, Digest: material.DigestOf([]byte(id)), Size: 10, Type: typ, Name: "секретное имя файла.pptx",
	}, ppT0)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, l := range lessons {
		if _, err := m.Link(l, ppT0); err != nil {
			e.t.Fatal(err)
		}
	}
	text, err := material.NewText(fs, false)
	if err != nil {
		e.t.Fatal(err)
	}
	e.lib.items[id], e.lib.texts[id] = m, text
	return m
}

func slides(lines ...string) []material.Fragment {
	fs := make([]material.Fragment, len(lines))
	for i, l := range lines {
		fs[i] = material.Fragment{Locator: material.Locator{Kind: material.LocatorSlide, N: i + 1}, Lines: []string{l}}
	}
	return fs
}

func (e *materialEnv) materialSnapshot(id shared.JobID, typ job.Type, m shared.MaterialID) job.Snapshot {
	e.t.Helper()
	req, err := values.NewMaterialRequest(m)
	if err != nil {
		e.t.Fatal(err)
	}
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: string(id), Payload: req.Payload(), MaxAttempts: 3}, ppT0)
	if err != nil {
		e.t.Fatal(err)
	}
	return j.Snapshot()
}

func TestMaterialSummaryJob(t *testing.T) {
	e := newMaterialEnv(t)
	e.addMaterial("m1", material.TypePPTX, slides("Реки и дельты", "Домашнее задание: глава 4"))
	e.llm.Answer(values.OperationSummary, summaryAnswer)

	if err := e.handle(e.materialSnapshot("j1", MaterialSummaryJobType, "m1")); err != nil {
		t.Fatal(err)
	}
	st, ok := e.summaries[summary.IDOfMaterial("m1")]
	if !ok || st.Material != "m1" || st.Transcript != "" || st.Lesson != "" || len(st.Sections) != 1 {
		t.Fatalf("summary %+v, stored %v", st, ok)
	}
	reqs := e.llm.RequestsFor(values.OperationSummary)
	if len(reqs) != 1 {
		t.Fatalf("%d summary requests", len(reqs))
	}
	data := reqs[0].UntrustedData()
	if !strings.Contains(data, "[Слайд 2]") || !strings.Contains(data, "Реки и дельты") || strings.Contains(data, "секретное") {
		t.Fatalf("request data:\n%s", data)
	}
	// The same job again (a retry) replaces nothing and is not charged twice.
	if err := e.handle(e.materialSnapshot("j1", MaterialSummaryJobType, "m1")); err != nil {
		t.Fatal(err)
	}
	if again := e.llm.RequestsFor(values.OperationSummary); again[1].IdempotencyKey() != reqs[0].IdempotencyKey() {
		t.Fatal("a retry of the job changed its LLM key")
	}
}

func TestMaterialHomeworkJob(t *testing.T) {
	e := newMaterialEnv(t)
	// Linked to exactly one lesson: the lesson anchors the dates and is the lesson of the assignments.
	e.addMaterial("m1", material.TypeDOCX, slides("Задание к семинару"), "l1")
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)

	if err := e.handle(e.materialSnapshot("j1", MaterialHomeworkJobType, "m1")); err != nil {
		t.Fatal(err)
	}
	list, err := ppAssignments{e.ppStore}.ListByMaterial(context.Background(), "m1")
	if err != nil || len(list) != 2 {
		t.Fatalf("assignments = %v, %v", list, err)
	}
	for i, a := range list {
		if o, ok := a.Origin(); !ok || o.Material != "m1" || o.Transcript != "" || o.Index != i || a.LessonID() != "l1" {
			t.Fatalf("assignment %d: origin %+v, lesson %s", i, o, a.LessonID())
		}
	}
	if data := e.llm.RequestsFor(values.OperationExtractHomework)[0].UntrustedData(); !strings.Contains(data, "Geography") {
		t.Fatalf("the linked lesson is not in the request:\n%s", data)
	}

	// A new job with a shorter answer withdraws the item it no longer has, as for a transcript.
	e.llm.Answer(values.OperationExtractHomework, `{"assignments":[{"title":"Read chapter 4","description":"Pages 40-52","due":"2026-10-16T23:59:00+03:00"}]}`)
	if err := e.handle(e.materialSnapshot("j2", MaterialHomeworkJobType, "m1")); err != nil {
		t.Fatal(err)
	}
	if list, _ := (ppAssignments{e.ppStore}).ListByMaterial(context.Background(), "m1"); len(list) != 1 {
		t.Fatalf("after the shorter answer: %d assignments", len(list))
	}
	// The transcript of the same env is untouched.
	if len(e.assignmentsOf("t1")) != 0 {
		t.Fatal("a material job wrote assignments of a transcript")
	}
}

func TestMaterialJobFailures(t *testing.T) {
	e := newMaterialEnv(t)
	e.addMaterial("empty", material.TypeTXT, nil)
	e.addMaterial("m1", material.TypeTXT, slides("x"))
	big := strings.Repeat("слово ", 100_000) // above the transcript limit of the request
	e.addMaterial("big", material.TypeTXT, material.FragmentsOf([]string{big}))
	truncated, err := material.NewText(slides("часть"), true)
	if err != nil {
		t.Fatal(err)
	}
	e.addMaterial("cut", material.TypeTXT, nil)
	e.lib.texts["cut"] = truncated
	e.lib.errs["broken"] = fmt.Errorf("doctext: %w", material.ErrTextUnreadable)
	e.lib.errs["damaged"] = fmt.Errorf("library: %w", material.ErrDamaged)
	e.lib.errs["removed"] = fmt.Errorf("library: %w", material.ErrRemoved)
	e.lib.errs["busy"] = errors.New("disk busy")

	tests := []struct {
		material shared.MaterialID
		want     error
		retry    bool
	}{
		{"nope", ErrMaterialNotFound, false},
		{"removed", ErrMaterialNotFound, false},
		{"broken", ErrMaterialUnreadable, false},
		{"damaged", ErrMaterialUnreadable, false},
		{"empty", ErrMaterialEmpty, false},
		{"cut", ErrContextRefused, false},
		{"big", ErrContextRefused, false},
		{"busy", ErrStorage, true},
	}
	for _, tt := range tests {
		err := e.handle(e.materialSnapshot("j-"+shared.JobID(tt.material), MaterialSummaryJobType, tt.material))
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.material, err, tt.want)
		}
	}
	if n := len(e.llm.Requests()); n != 0 {
		t.Fatalf("%d LLM calls for materials that cannot be sent", n)
	}

	// A bad payload, and a PostProcessor without the library, fail at once.
	j, _ := job.New(job.Spec{ID: "jx", Type: MaterialSummaryJobType, Key: "jx", Payload: `{"transcript_id":"t1"}`, MaxAttempts: 3}, ppT0)
	if err := e.handle(j.Snapshot()); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("transcript payload on a material job = %v", err)
	}
	plain := newPPEnv(t)
	if err := plain.handle(e.materialSnapshot("j9", MaterialSummaryJobType, "m1")); !errors.Is(err, ErrUnknownJobType) {
		t.Fatalf("without the library = %v", err)
	}
	d := plain.deps()
	d.Materials = e.lib
	if _, err := NewPostProcessor(d); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("materials without their assignments = %v", err)
	}
}

func TestMaterialJobsStart(t *testing.T) {
	e := newMaterialEnv(t)
	e.addMaterial("m1", material.TypeTXT, slides("текст"))
	e.addMaterial("pdf", material.TypePDF, nil)
	e.addMaterial("photo", material.TypeJPEG, nil)
	gone := e.addMaterial("gone", material.TypeTXT, slides("x"))
	if _, err := gone.Remove(ppT0); err != nil {
		t.Fatal(err)
	}
	mj, err := NewMaterialJobs(MaterialJobsDeps{
		Materials: e.lib, Jobs: ppJobs{e.ppStore}, Summaries: ppSummaries{e.ppStore}, Clock: e.clock, IDs: ppIDs{e.ppStore},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	id, created, err := mj.Start(ctx, "m1", WorkSummary)
	if err != nil || !created {
		t.Fatalf("Start = %s, %v, %v", id, created, err)
	}
	first := e.jobs[materialJobKey(MaterialSummaryJobType, "m1")]
	if first == nil || first.ID() != id || first.Payload() != `{"material_id":"m1"}` {
		t.Fatalf("job %+v", first)
	}
	// Pending: a second start is refused, nothing new.
	if _, _, err := mj.Start(ctx, "m1", WorkSummary); !errors.Is(err, job.ErrStillActive) {
		t.Fatalf("Start while pending = %v", err)
	}
	if ready, err := mj.SummaryReady(ctx, "m1"); err != nil || ready {
		t.Fatalf("SummaryReady before = %v, %v", ready, err)
	}

	// Done: the next start is a new job with a key derived from the first.
	e.llm.Answer(values.OperationSummary, summaryAnswer)
	if err := first.Claim(ppT0); err != nil {
		t.Fatal(err)
	}
	if err := e.pp.Handle(ctx, first.Snapshot(), nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Complete(ppT0); err != nil {
		t.Fatal(err)
	}
	if ready, err := mj.SummaryReady(ctx, "m1"); err != nil || !ready {
		t.Fatalf("SummaryReady after = %v, %v", ready, err)
	}
	e.clock.Advance(1)
	next, created, err := mj.Start(ctx, "m1", WorkSummary)
	if err != nil || !created || next == id {
		t.Fatalf("Start after done = %s, %v, %v", next, created, err)
	}

	// The homework search has jobs of its own.
	if _, created, err := mj.Start(ctx, "m1", WorkHomework); err != nil || !created {
		t.Fatalf("Start(homework) = %v, %v", created, err)
	}

	for name, tc := range map[string]struct {
		m    shared.MaterialID
		w    Work
		want error
	}{
		"pdf":          {"pdf", WorkSummary, ErrNoMaterialText},
		"photo":        {"photo", WorkHomework, ErrNoMaterialText},
		"removed":      {"gone", WorkSummary, material.ErrRemoved},
		"missing":      {"nope", WorkSummary, shared.ErrNotFound},
		"lecture text": {"m1", WorkLectureText, ErrUnknownWork},
	} {
		if _, _, err := mj.Start(ctx, tc.m, tc.w); !errors.Is(err, tc.want) {
			t.Errorf("%s: Start = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := NewMaterialJobs(MaterialJobsDeps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewMaterialJobs(empty) = %v", err)
	}
}
