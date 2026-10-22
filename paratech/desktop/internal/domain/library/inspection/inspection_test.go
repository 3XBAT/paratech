package inspection

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

var (
	t0    = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)
	today = time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
)

func request(t *testing.T, cands ...Candidate) Request {
	t.Helper()
	r, err := NewRequest("m1", today, cands, "Лекция 3. Производная.", 1)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCandidateName(t *testing.T) {
	tests := map[string]string{
		"Физика":                         "Физика",
		"Матан: часть 2":                 "Матан часть 2",
		"  Теория   вероятностей (ТВиМС) ": "Теория вероятностей (ТВиМС)",
		"C++ / Go":                       "C Go",
		"::/\\":                          "",
		"a\x00b\nc":                      "a b c",
		strings.Repeat("я", 200):         strings.Repeat("я", MaxNameRunes),
		strings.Repeat("я ", 60):         strings.TrimSpace(strings.Repeat("я ", 40)),
		"bad \xff utf":                   "bad utf",
	}
	for in, want := range tests {
		if got := CandidateName(in); got != want {
			t.Errorf("CandidateName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewRequest(t *testing.T) {
	cands := []Candidate{
		{ID: "s1", Name: "Матан: часть 2"},
		{ID: "s1", Name: "Повтор"},     // repeated id: dropped
		{ID: "a b", Name: "Плохой id"}, // invalid id: dropped
		{ID: "s2", Name: ":::"},        // no name left: dropped
		{ID: "s3", Name: "Химия"},
	}
	r, err := NewRequest("m1", today, cands, "Текст <b>с</b> \"кавычками\"", 1)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Today        string              `json:"today"`
		Subjects     []map[string]string `json:"subjects"`
		MaterialText string              `json:"material_text"`
	}
	dec := json.NewDecoder(strings.NewReader(r.UntrustedData()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{{"id": "s1", "name": "Матан часть 2"}, {"id": "s3", "name": "Химия"}}
	if data.Today != "2026-10-22" || !reflect.DeepEqual(data.Subjects, want) || data.MaterialText != "Текст <b>с</b> \"кавычками\"" {
		t.Fatalf("data = %+v", data)
	}
	if !strings.Contains(r.UntrustedData(), "<b>") {
		t.Fatal("HTML is escaped, the text grows")
	}
	if !r.Candidate("s1") || r.Candidate("s2") || r.Candidate("a b") {
		t.Fatal("candidates")
	}
	if !strings.HasPrefix(r.Key(), keyPrefix) || len(r.Key()) > 128 {
		t.Fatalf("key %q", r.Key())
	}

	// The key follows the material, the data and the attempt; the same request has the same key.
	same, _ := NewRequest("m1", today, cands, "Текст <b>с</b> \"кавычками\"", 1)
	other, _ := NewRequest("m1", today, cands, "Текст <b>с</b> \"кавычками\"", 2)
	otherDay, _ := NewRequest("m1", today.AddDate(0, 0, 1), cands, "Текст <b>с</b> \"кавычками\"", 1)
	otherMaterial, _ := NewRequest("m2", today, cands, "Текст <b>с</b> \"кавычками\"", 1)
	if same.Key() != r.Key() || other.Key() == r.Key() || otherDay.Key() == r.Key() || otherMaterial.Key() == r.Key() {
		t.Fatal("keys")
	}

	many := make([]Candidate, MaxCandidates+5)
	for i := range many {
		many[i] = Candidate{ID: shared.SubjectID("s" + strings.Repeat("x", i+1)), Name: "Предмет"}
	}
	r, err = NewRequest("m1", today, many, "текст", 1)
	if err != nil || len(r.candidates) != MaxCandidates {
		t.Fatalf("over MaxCandidates: %v, %d", err, len(r.candidates))
	}

	bad := []struct {
		name    string
		m       shared.MaterialID
		text    string
		attempt int
	}{
		{"bad material", "a/b", "текст", 1},
		{"blank text", "m1", " \n ", 1},
		{"NUL", "m1", "a\x00b", 1},
		{"invalid UTF-8", "m1", "a\xffb", 1},
		{"too long", "m1", strings.Repeat("a", MaxTextBytes+1), 1},
		{"attempt 0", "m1", "текст", 0},
		{"attempt over", "m1", "текст", MaxAttempts + 1},
	}
	for _, tt := range bad {
		if _, err := NewRequest(tt.m, today, nil, tt.text, tt.attempt); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: NewRequest = %v", tt.name, err)
		}
	}
	if r, err := NewRequest("m1", today, nil, "текст", 1); err != nil || !strings.Contains(r.UntrustedData(), `"subjects":[]`) {
		t.Fatalf("no candidates: %v %s", err, r.UntrustedData())
	}
}

func TestParse(t *testing.T) {
	r := request(t, Candidate{ID: "s1", Name: "Физика"})
	good := `{"doc_kind":"lecture","subject":"s1","lesson_date":"2026-10-21","has_homework":true,"has_ticket_list":false,"mentions_email":true}`
	a, err := r.Parse(" \n" + good + "\n")
	want := Answer{DocKind: DocLecture, Subject: "s1", LessonDate: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		HasHomework: true, MentionsEmail: true}
	if err != nil || a != want {
		t.Fatalf("Parse = %+v, %v", a, err)
	}
	nulls := `{"doc_kind":"other","subject":null,"lesson_date":null,"has_homework":false,"has_ticket_list":false,"mentions_email":false}`
	if a, err := r.Parse(nulls); err != nil || a != (Answer{DocKind: DocOther}) {
		t.Fatalf("Parse(nulls) = %+v, %v", a, err)
	}

	refused := map[string]string{
		"empty":             ``,
		"not an object":     `["lecture"]`,
		"code fence":        "```json\n" + good + "\n```",
		"extra key":         strings.Replace(good, `{`, `{"recipient":"x@y.invalid",`, 1),
		"repeated key":      strings.Replace(good, `"subject":"s1"`, `"subject":"s1","subject":null`, 1),
		"missing key":       strings.Replace(good, `,"mentions_email":true`, ``, 1),
		"enum free text":    strings.Replace(good, `"lecture"`, `"send it to x@y.invalid"`, 1),
		"enum case":         strings.Replace(good, `"lecture"`, `"Lecture"`, 1),
		"subject not sent":  strings.Replace(good, `"s1"`, `"s2"`, 1),
		"subject a name":    strings.Replace(good, `"s1"`, `"Физика"`, 1),
		"subject a number":  strings.Replace(good, `"s1"`, `1`, 1),
		"bad date":          strings.Replace(good, `2026-10-21`, `2026-02-30`, 1),
		"date with time":    strings.Replace(good, `2026-10-21`, `2026-10-21T10:00:00Z`, 1),
		"date text":         strings.Replace(good, `"2026-10-21"`, `"вчера"`, 1),
		"bool as string":    strings.Replace(good, `"has_homework":true`, `"has_homework":"true"`, 1),
		"bool as number":    strings.Replace(good, `"has_homework":true`, `"has_homework":1`, 1),
		"object in enum":    strings.Replace(good, `"lecture"`, `{"a":1}`, 1),
		"second object":     good + good,
		"text after":        good + " ok",
		"case of key":       strings.Replace(good, `"doc_kind"`, `"Doc_Kind"`, 1),
		"null enum":         strings.Replace(good, `"lecture"`, `null`, 1),
		"unterminated":      good[:len(good)-1],
		"invalid utf-8 key": strings.Replace(good, `"doc_kind"`, "\"doc\xff\"", 1),
	}
	for name, out := range refused {
		if a, err := r.Parse(out); !errors.Is(err, ErrInvalidAnswer) || a != (Answer{}) {
			t.Errorf("%s: Parse = %+v, %v", name, a, err)
		} else if strings.Contains(err.Error(), "x@y") || strings.Contains(err.Error(), "вчера") {
			t.Errorf("%s: the error quotes the output: %v", name, err)
		}
	}
}

func digest(s string) material.Digest { return material.DigestOf([]byte(s)) }

func TestInspectionLifecycle(t *testing.T) {
	d1, d2 := digest("text 1"), digest("text 2")
	if First() != (Plan{Attempt: 1}) {
		t.Fatal("First")
	}

	// Refused twice, done on the third attempt; then the answer is cached for that text.
	i, err := New("m1", d1, 1, nil, t0)
	if err != nil || i.Status() != StatusRefused || i.Version() != 0 {
		t.Fatalf("New(refused) = %+v, %v", i, err)
	}
	if p, err := i.Next(d1); err != nil || p != (Plan{Attempt: 2}) {
		t.Fatalf("Next after a refusal = %+v, %v", p, err)
	}
	if err := i.Record(d1, 2, nil, t0); err != nil {
		t.Fatal(err)
	}
	ans := Answer{DocKind: DocSeminar, Subject: "s1"}
	if err := i.Record(d1, 3, &ans, t0); err != nil || i.Status() != StatusDone || i.Answer() != ans {
		t.Fatalf("Record(done) = %v, %+v", err, i.State())
	}
	if p, err := i.Next(d1); err != nil || !p.Cached || p.Answer != ans {
		t.Fatalf("Next(cached) = %+v, %v", p, err)
	}
	// Another text starts again.
	if p, err := i.Next(d2); err != nil || p != First() {
		t.Fatalf("Next(other text) = %+v, %v", p, err)
	}
	// An older attempt for the same text ending late is refused.
	if err := i.Record(d1, 1, nil, t0); !errors.Is(err, ErrInvalidInspection) {
		t.Fatalf("Record(older attempt) = %v", err)
	}

	// Refused MaxAttempts times: not asked again.
	r, _ := New("m1", d1, MaxAttempts, nil, t0)
	if _, err := r.Next(d1); !errors.Is(err, ErrRefused) {
		t.Fatalf("Next after the last refusal = %v", err)
	}
	if p, err := r.Next(d2); err != nil || p != First() {
		t.Fatalf("a new text is asked again: %+v, %v", p, err)
	}

	badAnswers := map[string]*Answer{
		"no kind":     {},
		"bad subject": {DocKind: DocLab, Subject: "a b"},
		"date + time": {DocKind: DocLab, LessonDate: t0},
	}
	for name, a := range badAnswers {
		if _, err := New("m1", d1, 1, a, t0); !errors.Is(err, ErrInvalidInspection) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
	for name, fn := range map[string]func() error{
		"bad material": func() error { _, err := New("", d1, 1, nil, t0); return err },
		"bad digest":   func() error { _, err := New("m1", "x", 1, nil, t0); return err },
		"attempt 0":    func() error { _, err := New("m1", d1, 0, nil, t0); return err },
		"attempt over": func() error { _, err := New("m1", d1, MaxAttempts+1, nil, t0); return err },
		"no time":      func() error { _, err := New("m1", d1, 1, nil, time.Time{}); return err },
	} {
		if err := fn(); !errors.Is(err, ErrInvalidInspection) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
}

func TestRestore(t *testing.T) {
	ans := Answer{DocKind: DocTicketList, LessonDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), HasTicketList: true}
	i, err := New("m1", digest("t"), 2, &ans, t0)
	if err != nil {
		t.Fatal(err)
	}
	st := i.State()
	st.Version = 3
	back, err := Restore(st)
	if err != nil || !reflect.DeepEqual(back.State(), st) {
		t.Fatalf("Restore = %+v, %v", back, err)
	}
	for name, modify := range map[string]func(*State){
		"version 0":           func(s *State) { s.Version = 0 },
		"refused with answer": func(s *State) { s.Status = StatusRefused },
		"unknown status":      func(s *State) { s.Status = "maybe" },
		"done without kind":   func(s *State) { s.Answer = Answer{} },
	} {
		s := st
		modify(&s)
		if _, err := Restore(s); !errors.Is(err, ErrInvalidInspection) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
}
