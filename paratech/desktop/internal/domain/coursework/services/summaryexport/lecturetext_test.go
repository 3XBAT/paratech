package summaryexport

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
)

func lectureBlock(t *testing.T, heading string, start int64, paragraphs ...string) lecturetext.Block {
	t.Helper()
	b, err := lecturetext.NewBlock(heading, start, paragraphs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRenderLectureText(t *testing.T) {
	doc := LectureDocument{
		Title: "География", Start: time.Date(2026, 10, 14, 10, 1, 0, 0, msk), Duration: 85 * time.Minute,
		Blocks: []lecturetext.Block{
			lectureBlock(t, "Введение", 0, "Сегодня говорим о реках.", "Начнём с дельт."),
			lectureBlock(t, "Дельты", 3_725_000, "1. Дельта — устье. <b>Важно</b> [ссылка](http://x.example)"),
		},
		Times: true,
	}
	tests := []struct {
		name string
		doc  LectureDocument
		f    Format
		want string
	}{
		{"text with times", doc, FormatText, "География\n14.10.2026, 10:01 · 1 ч 25 мин\n\n" +
			"[00:00] Введение\n\nСегодня говорим о реках.\n\nНачнём с дельт.\n\n" +
			"[1:02:05] Дельты\n\n1. Дельта — устье. <b>Важно</b> [ссылка](http://x.example)\n"},
		{"markdown escapes the model text", doc, FormatMarkdown, "# География\n\n14\\.10.2026, 10:01 · 1 ч 25 мин\n\n" +
			"## \\[00:00\\] Введение\n\nСегодня говорим о реках.\n\nНачнём с дельт.\n\n" +
			"## \\[1:02:05\\] Дельты\n\n1\\. Дельта — устье. \\<b\\>Важно\\</b\\> \\[ссылка\\](http:\\//x.example)\n"},
		{"without times and head", LectureDocument{Blocks: doc.Blocks[:1]}, FormatText,
			"Введение\n\nСегодня говорим о реках.\n\nНачнём с дельт.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderLectureText(tc.doc, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}

	if _, err := RenderLectureText(doc, "html"); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("unknown format: %v", err)
	}
	md, _ := RenderLectureText(doc, FormatMarkdown)
	if strings.Contains(md, "<b>") || strings.Contains(md, "[ссылка]") || strings.Contains(md, "://") {
		t.Fatal("markdown keeps markup of the model")
	}
}

func TestBlockTime(t *testing.T) {
	for ms, want := range map[int64]string{0: "00:00", 65_000: "01:05", 3_599_000: "59:59", 3_600_000: "1:00:00", -1: "00:00"} {
		if got := BlockTime(ms); got != want {
			t.Errorf("BlockTime(%d) = %q, want %q", ms, got, want)
		}
	}
}
