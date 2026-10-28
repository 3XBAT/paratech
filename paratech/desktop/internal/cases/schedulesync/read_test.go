package schedulesync

import (
	"context"
	"testing"
)

func TestLessonsStartingBetween(t *testing.T) {
	e := newEnv(t)
	e.seed(t, "l-before", "csv:a", content("A", at(-1)))
	e.seed(t, "l-from", "csv:b", content("B", at(0)))
	e.seed(t, "l-in", "csv:c", content("C", at(5)))
	e.seed(t, "l-to", "csv:d", content("D", at(24)))

	got, err := e.svc.LessonsStartingBetween(context.Background(), at(0), at(24))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID() != "l-from" || got[1].ID() != "l-in" {
		t.Fatalf("lessons in [from, to) = %v", got)
	}
}
