package thread

import (
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func TestCardLines(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []CardLine
	}{
		{"no reply", NoReply, nil},
		{"empty", "", nil},
		{"reply", "reply: горутина — это лёгкий поток", []CardLine{{Kind: CardReply, Text: "горутина — это лёгкий поток"}}},
		{"empty reply", "reply: ", []CardLine{{Kind: CardReply}}},
		{"plan", "plan: Сначала найти задание", []CardLine{{Kind: CardPlan, Text: "Сначала найти задание"}}},
		{"asked", "asked: questions to the student", []CardLine{{Kind: CardAsked}}},
		{"cannot_do with both parts", "cannot_do: your_part: отправить письмо | выбрать тему; our_part: черновик",
			[]CardLine{{Kind: CardCannotDo, Yours: []string{"отправить письмо", "выбрать тему"}, Ours: []string{"черновик"}}}},
		{"cannot_do with ours only", "cannot_do: our_part: черновик | план",
			[]CardLine{{Kind: CardCannotDo, Ours: []string{"черновик", "план"}}}},
		{"cannot_do with yours only", "cannot_do: your_part: письмо",
			[]CardLine{{Kind: CardCannotDo, Yours: []string{"письмо"}}}},
		{"cannot_do without parts", "cannot_do: ", []CardLine{{Kind: CardCannotDo}}},
		{"actions line and NoReply give nothing", NoReply + "\nactions: list_subjects ok, search error", nil},
		{"cards then actions", "plan: план\nreply: готово\nactions: create_folder ok",
			[]CardLine{{Kind: CardPlan, Text: "план"}, {Kind: CardReply, Text: "готово"}}},
		{"a line cut at the limit is not a card", "reply: да\nrepl", []CardLine{{Kind: CardReply, Text: "да"}}},
		{"an unknown line", "something: else", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CardLines(tt.line); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("CardLines(%q) = %+v, want %+v", tt.line, got, tt.want)
			}
		})
	}
}

// Every card kind read back from the line Reply builds: the texts come out as written, with line breaks as spaces.
func TestCardLinesRoundTripWithReply(t *testing.T) {
	plan := argsJSON(t, map[string]any{
		"summary": "Сначала найти задание,\nпотом написать",
		"steps":   []map[string]any{{"tool": "list_assignments", "arguments": map[string]any{}}},
	})
	cannot := argsJSON(t, map[string]any{"your_part": []string{"отправить письмо", "выбрать тему"}, "our_part": []string{"черновик"}})
	asked := argsJSON(t, map[string]any{"questions": []map[string]any{
		{"kind": "choice", "question": "Какая тема?", "options": []string{"первая", "вторая"}},
	}})
	tests := []struct {
		name    string
		request []values.HistoryEntry
		want    []CardLine
	}{
		{"reply", []values.HistoryEntry{
			entry(1, "вопрос", nil), echo(1, [2]string{"reply", `{"text":"горутина —\nлёгкий поток"}`}), results(1, values.ShownResult("c0")),
		}, []CardLine{{Kind: CardReply, Text: "горутина — лёгкий поток"}}},
		{"plan", []values.HistoryEntry{entry(1, "сделай", nil), echo(1, [2]string{"show_plan", plan})},
			[]CardLine{{Kind: CardPlan, Text: "Сначала найти задание, потом написать"}}},
		{"cannot_do", []values.HistoryEntry{entry(1, "сделай", nil), echo(1, [2]string{"cannot_do", cannot}), results(1, values.ShownResult("c0"))},
			[]CardLine{{Kind: CardCannotDo, Yours: []string{"отправить письмо", "выбрать тему"}, Ours: []string{"черновик"}}}},
		{"asked", []values.HistoryEntry{entry(1, "сделай", nil), echo(1, [2]string{"ask_questions", asked})},
			[]CardLine{{Kind: CardAsked}}},
		{"no card", []values.HistoryEntry{entry(1, "вопрос", nil), echo(1, [2]string{"list_subjects", `{}`}), results(1, ok("c0"))}, nil},
		{"many cards and actions", []values.HistoryEntry{
			entry(1, "сделай", nil), echo(1, [2]string{"show_plan", plan}), results(1, values.PlanResult("c0", true, "")),
			echo(1, [2]string{"create_folder", `{"name":"Лабы"}`}), results(1, ok("c0")),
			echo(1, [2]string{"reply", `{"text":"Готово"}`}), results(1, values.ShownResult("c0")),
		}, []CardLine{{Kind: CardPlan, Text: "Сначала найти задание, потом написать"}, {Kind: CardReply, Text: "Готово"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := Reply(turnsOf(tt.request))
			if got := CardLines(line); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("CardLines(Reply) of %q = %+v, want %+v", line, got, tt.want)
			}
		})
	}
}

// A part text that holds the separator is split there: the known loss of the flat line.
func TestCardLinesSplitAPartHoldingTheSeparator(t *testing.T) {
	got := CardLines("cannot_do: your_part: " + strings.Join([]string{"a | b", "c"}, " | "))
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got[0].Yours, want) {
		t.Fatalf("yours = %q, want %q", got[0].Yours, want)
	}
}
