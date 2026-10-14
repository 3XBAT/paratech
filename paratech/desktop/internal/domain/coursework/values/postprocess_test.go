package values

import (
	"errors"
	"testing"
)

func TestPostProcessPayloadRoundTrip(t *testing.T) {
	r, err := NewPostProcessRequest("t-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Payload(); got != `{"transcript_id":"t-1"}` {
		t.Fatalf("Payload = %s", got)
	}
	back, err := ParsePostProcessPayload(r.Payload())
	if err != nil || back != r {
		t.Fatalf("ParsePostProcessPayload = %v, %v", back, err)
	}
}

func TestParsePostProcessPayloadRejects(t *testing.T) {
	for name, payload := range map[string]string{
		"empty":         "",
		"empty object":  "{}",
		"null id":       `{"transcript_id":null}`,
		"empty id":      `{"transcript_id":""}`,
		"bad id":        `{"transcript_id":"a/b"}`,
		"number id":     `{"transcript_id":1}`,
		"unknown field": `{"transcript_id":"t","recording_id":"r"}`,
		"trailing data": `{"transcript_id":"t"} {}`,
		"array":         `["t"]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePostProcessPayload(payload); !errors.Is(err, ErrInvalidPostProcessRequest) {
				t.Fatalf("error = %v, want ErrInvalidPostProcessRequest", err)
			}
		})
	}
	if _, err := NewPostProcessRequest(""); !errors.Is(err, ErrInvalidPostProcessRequest) {
		t.Fatalf("NewPostProcessRequest(\"\") error = %v", err)
	}
}
