package values

import "testing"

func TestHomeworkMarkdown(t *testing.T) {
	cases := []struct{ title, body, want string }{
		{"Rivers", "Rivers flow to the sea.", "# Rivers\n\nRivers flow to the sea.\n"},
		{"  Rivers \n", "\n1. Deltas.\n2. Seas.\n\n", "# Rivers\n\n1. Deltas.\n2. Seas.\n"},
	}
	for _, tc := range cases {
		if got := HomeworkMarkdown(tc.title, tc.body); got != tc.want {
			t.Errorf("HomeworkMarkdown(%q, %q) = %q, want %q", tc.title, tc.body, got, tc.want)
		}
	}
}
