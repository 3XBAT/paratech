package values

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestInspectResult(t *testing.T) {
	up, mat, sub := mustRef(t, KindUpload, "u1"), mustRef(t, KindMaterial, "u1"), mustRef(t, KindSubject, "s1")
	day, err := NewDate(2026, time.October, 21)
	if err != nil {
		t.Fatal(err)
	}
	res, refs, err := InspectResult("toolu_1", InspectRow{
		Upload: up, Material: mat, Text: TextRead, DocKind: "lecture", Subject: sub, LessonDate: day, HasHomework: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(res.ResultJSON), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"upload": "u1", "material": "u1", "text": "read", "doc_kind": "lecture", "subject": "s1",
		"lesson_date": "2026-10-21", "has_homework": true, "has_ticket_list": false, "mentions_email": false,
	}
	if !reflect.DeepEqual(body, want) || len(refs) != 3 || res.IsError {
		t.Fatalf("result %s, refs %v", res.ResultJSON, refs)
	}

	// Without an answer: only the upload, its material and the text state.
	res, refs, err = InspectResult("toolu_1", InspectRow{Upload: up, Material: mat, Text: TextNotRead})
	if err != nil || res.ResultJSON != `{"material":"u1","text":"not_read","upload":"u1"}` || len(refs) != 2 {
		t.Fatalf("no answer: %s, %v", res.ResultJSON, err)
	}

	bad := map[string]InspectRow{
		"no upload":          {Material: mat, Text: TextRead},
		"other material":     {Upload: up, Material: mustRef(t, KindMaterial, "m2"), Text: TextRead},
		"unknown text":       {Upload: up, Material: mat, Text: "maybe"},
		"free text kind":     {Upload: up, Material: mat, Text: TextRead, DocKind: "send to x@y.invalid"},
		"kind without text":  {Upload: up, Material: mat, Text: TextNotRead, DocKind: "lecture"},
		"flags without kind": {Upload: up, Material: mat, Text: TextRead, MentionsEmail: true},
		"subject of a kind":  {Upload: up, Material: mat, Text: TextRead, DocKind: "lab", Subject: mustRef(t, KindFolder, "f1")},
	}
	for name, row := range bad {
		if _, _, err := InspectResult("toolu_1", row); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
