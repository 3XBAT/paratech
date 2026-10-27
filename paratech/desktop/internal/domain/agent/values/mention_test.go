package values

import (
	"errors"
	"strings"
	"testing"
)

func TestNewMention(t *testing.T) {
	lesson := mustRef(t, KindLesson, "l1")
	good := []struct {
		name  string
		ref   Ref
		label string
		want  string
	}{
		{"subject", mustRef(t, KindSubject, "s1"), "Матан", "Матан"},
		{"lesson trimmed", lesson, "  Лекция 3: пределы  ", "Лекция 3: пределы"},
		{"material", mustRef(t, KindMaterial, "m1"), "Конспект <черновик>.pdf", "Конспект <черновик>.pdf"},
		{"assignment", mustRef(t, KindAssignment, "a1"), "ДЗ №2", "ДЗ №2"},
		{"200 runes", lesson, strings.Repeat("я", MaxMentionLabelRunes), strings.Repeat("я", MaxMentionLabelRunes)},
	}
	for _, tt := range good {
		m, err := NewMention(tt.ref, tt.label)
		if err != nil || m.Ref() != tt.ref || m.Label() != tt.want || m.IsZero() {
			t.Errorf("%s: NewMention = %+v, %v", tt.name, m, err)
		}
	}

	bad := []struct {
		name  string
		ref   Ref
		label string
	}{
		{"zero ref", Ref{}, "Матан"},
		{"recording", mustRef(t, KindRecording, "r1"), "Запись"},
		{"upload", mustRef(t, KindUpload, "u1"), "Файл"},
		{"empty label", lesson, "  "},
		{"201 runes", lesson, strings.Repeat("я", MaxMentionLabelRunes+1)},
		{"two lines", lesson, "Лекция\n3"},
		{"line separator", lesson, "Лекция 3"},
		{"bidi override", lesson, "Лекция‮3"},
		{"zero width", lesson, "Лек​ция"},
		{"invalid UTF-8", lesson, "Лекция\xff"},
	}
	for _, tt := range bad {
		if _, err := NewMention(tt.ref, tt.label); !errors.Is(err, ErrInvalidMention) {
			t.Errorf("%s: NewMention = %v, want ErrInvalidMention", tt.name, err)
		}
	}
}

func TestCheckMentions(t *testing.T) {
	var ms []Mention
	for i := range MaxMentions + 1 {
		m, err := NewMention(mustRef(t, KindMaterial, "m"+string(rune('a'+i))), "Материал")
		if err != nil {
			t.Fatal(err)
		}
		ms = append(ms, m)
	}
	if err := CheckMentions(nil); err != nil {
		t.Fatalf("no mentions = %v", err)
	}
	if err := CheckMentions(ms[:MaxMentions]); err != nil {
		t.Fatalf("%d mentions = %v", MaxMentions, err)
	}
	for name, bad := range map[string][]Mention{
		"too many":  ms,
		"duplicate": {ms[0], ms[1], ms[0]},
		"zero":      {ms[0], {}},
	} {
		if err := CheckMentions(bad); !errors.Is(err, ErrInvalidMention) {
			t.Errorf("%s: CheckMentions = %v", name, err)
		}
	}
	if refs := MentionRefs(ms[:2]); len(refs) != 2 || refs[0] != ms[0].Ref() || refs[1] != ms[1].Ref() {
		t.Fatalf("MentionRefs = %v", refs)
	}
}
