package wails

import (
	"context"
	"encoding/json"
	"testing"

	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/summary"
)

// TASK-101: the page gets the sections with their headings, texts and points; lists are never null.
func TestSummaryViewSections(t *testing.T) {
	content, err := summary.NewSectionedContent([]summary.Section{
		{Heading: "<b>Реки</b>", Text: "Реки текут.\n\nВ море.", Points: []string{"Исток", "Устье"}},
		{Heading: "Озёра", Text: "Озёра стоят."},
	}, []string{"Вода"})
	if err != nil {
		t.Fatal(err)
	}
	f := newSummaryFixture()
	f.uc.view = summaryview.View{
		Recording: "r1", Transcribed: true,
		Summary: summaryview.Summary{Step: summaryview.Step{State: summaryview.StateDone}, Ready: true, Content: content},
	}
	got, err := f.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Text      string           `json:"text"`
		Sections  []SummarySection `json:"sections"`
		KeyPoints []string         `json:"keyPoints"`
	}{got.Summary.Text, got.Summary.Sections, got.Summary.KeyPoints})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"text":"\u003cb\u003eРеки\u003c/b\u003e:\nРеки текут.\n\nВ море.\n- Исток\n- Устье\n\nОзёра:\nОзёра стоят.",` +
		`"sections":[{"heading":"\u003cb\u003eРеки\u003c/b\u003e","text":"Реки текут.\n\nВ море.","points":["Исток","Устье"]},` +
		`{"heading":"Озёра","text":"Озёра стоят.","points":[]}],"keyPoints":["Вода"]}`
	if string(data) != want {
		t.Fatalf("summary =\n%s\nwant\n%s", data, want)
	}
}
