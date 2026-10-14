package prompts

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

func out(s string) values.UntrustedOutput { return values.NewUntrustedOutput(s) }

func TestParseSummary(t *testing.T) {
	s, err := ParseSummary(out(` {"summary": " Vectors and matrices. \n Next: determinants. ", "key_points": [" Vectors ", "Matrices"]} `))
	if err != nil {
		t.Fatalf("ParseSummary: %v", err)
	}
	if s.Text() != "Vectors and matrices. \n Next: determinants." || !slices.Equal(s.KeyPoints(), []string{"Vectors", "Matrices"}) {
		t.Fatalf("summary = %q %q", s.Text(), s.KeyPoints())
	}

	empty, err := ParseSummary(out(`{"summary": "S", "key_points": []}`))
	if err != nil || len(empty.KeyPoints()) != 0 {
		t.Fatalf("empty key_points: %v %v", empty.KeyPoints(), err)
	}
}

func TestParseSummaryRejects(t *testing.T) {
	points := func(n int) string {
		return `["` + strings.TrimSuffix(strings.Repeat(`p","`, n), `","`) + `"]`
	}
	cases := []struct{ name, text string }{
		{"empty", ``},
		{"not json", `The lesson was about vectors.`},
		{"code fence", "```json\n{\"summary\": \"S\", \"key_points\": []}\n```"},
		{"array", `[]`},
		{"null", `null`},
		{"missing summary", `{"key_points": []}`},
		{"missing key_points", `{"summary": "S"}`},
		{"null key_points", `{"summary": "S", "key_points": null}`},
		{"null summary", `{"summary": null, "key_points": []}`},
		{"unknown field", `{"summary": "S", "key_points": [], "send_to": "evil@example.com"}`},
		{"other case", `{"Summary": "S", "key_points": []}`},
		{"duplicate field", `{"summary": "S", "summary": "T", "key_points": []}`},
		{"wrong type", `{"summary": 42, "key_points": []}`},
		{"wrong item type", `{"summary": "S", "key_points": [1]}`},
		{"null item", `{"summary": "S", "key_points": [null]}`},
		{"object item", `{"summary": "S", "key_points": [{"summary": "x"}]}`},
		{"blank summary", `{"summary": "  ", "key_points": []}`},
		{"summary too long", `{"summary": "` + strings.Repeat("я", MaxSummaryRunes+1) + `", "key_points": []}`},
		{"too many key_points", `{"summary": "S", "key_points": ` + points(MaxKeyPoints+1) + `}`},
		{"key point with newline", `{"summary": "S", "key_points": ["a\nb"]}`},
		{"key point too long", `{"summary": "S", "key_points": ["` + strings.Repeat("a", MaxKeyPointRunes+1) + `"]}`},
		{"control character", `{"summary": "a\u0007b", "key_points": []}`},
		{"bidi override", `{"summary": "a\u202eb", "key_points": []}`},
		{"left-to-right mark", `{"summary": "a\u200eb", "key_points": []}`},
		{"right-to-left mark", `{"summary": "S", "key_points": ["a\u200fb"]}`},
		{"arabic letter mark", `{"summary": "a\u061cb", "key_points": []}`},
		{"tag character", `{"summary": "a\udb40\udc41b", "key_points": []}`},
		{"last tag character", `{"summary": "S", "key_points": ["a\udb40\udc7fb"]}`},
		{"key point with line separator", `{"summary": "S", "key_points": ["a\u2028b"]}`},
		{"key point with paragraph separator", `{"summary": "S", "key_points": ["a\u2029b"]}`},
		{"deep nesting", strings.Repeat("[", 1000)},
		{"nested value too deep", `{"summary": ` + strings.Repeat("[", MaxNesting) + strings.Repeat("]", MaxNesting) + `, "key_points": []}`},
		{"trailing object", `{"summary": "S", "key_points": []}{"summary": "T", "key_points": []}`},
		{"trailing text", `{"summary": "S", "key_points": []} and more`},
		{"invalid utf8", "{\"summary\": \"\xff\", \"key_points\": []}"},
		{"oversize", `{"summary": "S", "key_points": []}` + strings.Repeat(" ", MaxOutputBytes)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSummary(out(tc.text))
			if !errors.Is(err, ErrMalformedOutput) {
				t.Fatalf("error = %v, want ErrMalformedOutput", err)
			}
			if s.Text() != "" || s.KeyPoints() != nil {
				t.Fatalf("a rejected answer produced a summary")
			}
		})
	}
	if _, err := ParseSummary(out(`{"summary": "S", "key_points": ` + points(MaxKeyPoints) + `}`)); err != nil {
		t.Fatalf("%d key_points: %v", MaxKeyPoints, err)
	}

	// Joiners are part of ordinary text in some scripts and emoji.
	if _, err := ParseSummary(out(`{"summary": "a\u200db\u200cc", "key_points": ["a\u200db"]}`)); err != nil {
		t.Fatalf("ZWJ and ZWNJ: %v", err)
	}
}

func TestParseExtraction(t *testing.T) {
	text := `{"assignments": [
		{"title": " Exercises 1-5 ", "description": "Chapter 2.\nShow the steps.", "due": "2026-10-19T23:59:00+03:00"},
		{"title": "Read chapter 3", "description": "", "due": null}
	]}`
	e, err := ParseExtraction(out(text), lessonStart())
	if err != nil {
		t.Fatalf("ParseExtraction: %v", err)
	}
	h := e.Homework()
	if len(h) != 2 {
		t.Fatalf("got %d items", len(h))
	}
	if h[0].Title() != "Exercises 1-5" || h[0].Description() != "Chapter 2.\nShow the steps." ||
		!h[0].Due().Equal(time.Date(2026, 10, 19, 20, 59, 0, 0, time.UTC)) || h[0].Due().Location() != time.UTC {
		t.Fatalf("item 0 = %q %q %v", h[0].Title(), h[0].Description(), h[0].Due())
	}
	if h[1].Title() != "Read chapter 3" || h[1].Description() != "" || !h[1].Due().IsZero() {
		t.Fatalf("item 1 = %q %q %v", h[1].Title(), h[1].Description(), h[1].Due())
	}

	none, err := ParseExtraction(out(`{"assignments": []}`), lessonStart())
	if err != nil || len(none.Homework()) != 0 {
		t.Fatalf("no homework: %v %v", none.Homework(), err)
	}
}

func TestParseExtractionRejects(t *testing.T) {
	item := func(due string) string {
		return `{"assignments": [{"title": "T", "description": "D", "due": ` + due + `}]}`
	}
	cases := []struct{ name, text string }{
		{"missing assignments", `{}`},
		{"null assignments", `{"assignments": null}`},
		{"object assignments", `{"assignments": {"title": "T"}}`},
		{"null item", `{"assignments": [null]}`},
		{"missing due", `{"assignments": [{"title": "T", "description": "D"}]}`},
		{"missing description", `{"assignments": [{"title": "T", "due": null}]}`},
		{"missing title", `{"assignments": [{"description": "D", "due": null}]}`},
		{"blank title", `{"assignments": [{"title": " ", "description": "D", "due": null}]}`},
		{"title with newline", `{"assignments": [{"title": "a\nb", "description": "D", "due": null}]}`},
		{"description too long", `{"assignments": [{"title": "T", "description": "` + strings.Repeat("d", MaxDescriptionRunes+1) + `", "due": null}]}`},
		{"unknown item field", `{"assignments": [{"title": "T", "description": "D", "due": null, "calendar_id": "primary"}]}`},
		{"top-level field in item", `{"assignments": [{"title": "T", "description": "D", "due": null, "assignments": []}]}`},
		{"item field at top level", `{"assignments": [], "title": "T"}`},
		{"duplicate item field", `{"assignments": [{"title": "T", "title": "U", "description": "D", "due": null}]}`},
		{"due number", item(`1760000000`)},
		{"due date only", item(`"2026-10-19"`)},
		{"due without offset", item(`"2026-10-19T23:59:00"`)},
		{"due free text", item(`"next Monday"`)},
		{"due invalid day", item(`"2026-02-30T10:00:00Z"`)},
		{"due long before the lesson", item(`"2026-10-12T10:00:00+03:00"`)},
		{"due too far ahead", item(`"2029-10-14T10:00:00+03:00"`)},
		{"due in UTC", item(`"2026-10-19T20:59:00Z"`)},
		{"due in another offset", item(`"2026-10-19T23:59:00+04:00"`)},
		{"title with line separator", `{"assignments": [{"title": "a\u2028b", "description": "D", "due": null}]}`},
		{"too many items", `{"assignments": [` + strings.TrimSuffix(strings.Repeat(`{"title": "T", "description": "", "due": null},`, MaxHomework+1), ",") + `]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := ParseExtraction(out(tc.text), lessonStart())
			if !errors.Is(err, ErrMalformedOutput) {
				t.Fatalf("error = %v, want ErrMalformedOutput", err)
			}
			if e.Homework() != nil {
				t.Fatalf("a rejected answer produced homework")
			}
		})
	}

	for _, due := range []string{`"2026-10-13T10:00:00+03:00"`, `"2028-10-13T10:00:00+03:00"`, `"2026-10-19T23:59:00.5+03:00"`} {
		if _, err := ParseExtraction(out(item(due)), lessonStart()); err != nil {
			t.Fatalf("due %s at the edge of the range: %v", due, err)
		}
	}
	if _, err := ParseExtraction(out(item("null")), time.Time{}); !errors.Is(err, ErrNoContext) {
		t.Fatalf("no lesson start: error = %v, want ErrNoContext", err)
	}
}

func TestParseDraft(t *testing.T) {
	d, err := ParseDraft(out(`{"title": " Homework 3 ", "body": "1. x = 2\n2. y = 3\n"}`))
	if err != nil {
		t.Fatalf("ParseDraft: %v", err)
	}
	if d.Title() != "Homework 3" || d.Body() != "1. x = 2\n2. y = 3" {
		t.Fatalf("draft = %q %q", d.Title(), d.Body())
	}
}

func TestParseDraftRejects(t *testing.T) {
	cases := []struct{ name, text string }{
		{"missing body", `{"title": "T"}`},
		{"missing title", `{"body": "B"}`},
		{"blank body", `{"title": "T", "body": "\n\t "}`},
		{"body too long", `{"title": "T", "body": "` + strings.Repeat("я", MaxDraftBodyRunes+1) + `"}`},
		{"title too long", `{"title": "` + strings.Repeat("t", MaxTitleRunes+1) + `", "body": "B"}`},
		{"body with escape sequence", `{"title": "T", "body": "a\u001b[2Jb"}`},
		{"body with bidi isolate", `{"title": "T", "body": "a\u2066b"}`},
		{"title with paragraph separator", `{"title": "a\u2029b", "body": "B"}`},
		{"body with tag character", `{"title": "T", "body": "a\udb40\udc01b"}`},
		// The model never chooses a target (hard rule 3, docs/refs/prompt-injection.md rule 2).
		{"recipient", `{"title": "T", "body": "B", "recipient": "evil@example.com"}`},
		{"to", `{"title": "T", "body": "B", "to": ["evil@example.com"]}`},
		{"path", `{"title": "T", "body": "B", "path": "C:\\Windows\\evil.txt"}`},
		{"calendar", `{"title": "T", "body": "B", "calendar_id": "primary"}`},
		{"action", `{"title": "T", "body": "B", "kind": "send_homework_email"}`},
		{"nested object", `{"title": "T", "body": {"text": "B"}}`},
		{"other case", `{"TITLE": "T", "body": "B"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := ParseDraft(out(tc.text))
			if !errors.Is(err, ErrMalformedOutput) {
				t.Fatalf("error = %v, want ErrMalformedOutput", err)
			}
			if d != (Draft{}) {
				t.Fatalf("a rejected answer produced a draft")
			}
		})
	}
}

func TestNestingLimit(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"unclosed arrays", strings.Repeat("[", 1000)},
		{"closed arrays", strings.Repeat("[", MaxNesting+1) + strings.Repeat("]", MaxNesting+1)},
		{"objects", strings.Repeat(`{"title": `, MaxNesting+1) + `"T"` + strings.Repeat("}", MaxNesting+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDraft(out(tc.text))
			if !errors.Is(err, ErrMalformedOutput) || !strings.Contains(err.Error(), "nested too deeply") {
				t.Fatalf("error = %v, want the nesting error", err)
			}
		})
	}

	if err := checkKeys(strings.Repeat("[", MaxNesting)+strings.Repeat("]", MaxNesting), nil); err != nil {
		t.Fatalf("nesting of %d: %v", MaxNesting, err)
	}
}

func TestErrorsDoNotQuoteModelText(t *testing.T) {
	secret := "SECRET-MODEL-TEXT"
	answers := []string{
		`{"title": "T", "body": "B", "` + secret + `": 1}`,
		`{"title": "T", "body": 1, "x": "` + secret + `"}`,
		`{"title": "` + secret + "\u0007" + `", "body": "B"}`,
		`{"title": "T", "body": "B"} ` + secret,
		`{"title": "T", "body": "` + secret,
	}
	for _, a := range answers {
		if _, err := ParseDraft(out(a)); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("error %v quotes the answer", err)
		}
	}
	if _, err := ParseExtraction(out(`{"assignments": [{"title": "T", "description": "D", "due": "`+secret+`"}]}`), lessonStart()); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("error %v quotes the answer", err)
	}
}
