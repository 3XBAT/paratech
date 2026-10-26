package values

import (
	"errors"
	"testing"
)

func TestFreeWorkCard(t *testing.T) {
	tests := []struct {
		name string
		card FreeWorkCard
		ok   bool
	}{
		{"valid", FreeWorkCard{Thread: "th1", Turn: 2, CallID: "toolu_01"}, true},
		{"no thread", FreeWorkCard{Turn: 2, CallID: "c1"}, false},
		{"no request", FreeWorkCard{Thread: "th1", CallID: "c1"}, false},
		{"bad call id", FreeWorkCard{Thread: "th1", Turn: 1, CallID: "c 1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.card.Validate()
			if tt.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidCard)) {
				t.Fatalf("Validate = %v", err)
			}
			if tt.card.Kind() != CardFreeWork {
				t.Fatalf("kind %s", tt.card.Kind())
			}
		})
	}
}

// The planner gets one fixed field for a call that waits on the card: never the topic.
func TestFreeWorkResult(t *testing.T) {
	r, err := FreeWorkResult("c1")
	if err != nil || r.ResultJSON != `{"status":"awaiting_student"}` || r.IsError || !IsFreeWorkResult(r) {
		t.Fatalf("FreeWorkResult = %+v, %v", r, err)
	}
	if _, err := FreeWorkResult(""); err == nil {
		t.Fatal("a result without a call id")
	}
	for _, other := range []ToolResult{
		{CallID: "c1", ResultJSON: `{"status":"awaiting_student"}`, IsError: true},
		{CallID: "c1", ResultJSON: `{"status": "awaiting_student"}`},
		RefusalResult("c1", RefuseUnavailable),
		ShownResult("c1"),
	} {
		if IsFreeWorkResult(other) {
			t.Fatalf("%+v taken for the free-work result", other)
		}
	}
}
