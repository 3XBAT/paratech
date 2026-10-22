package values

import (
	"errors"
	"testing"
)

func TestMaterialPayload(t *testing.T) {
	r, err := NewMaterialRequest("m1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Payload() != `{"material_id":"m1"}` {
		t.Fatalf("payload %s", r.Payload())
	}
	back, err := ParseMaterialPayload(r.Payload())
	if err != nil || back != r {
		t.Fatalf("parse = %+v, %v", back, err)
	}
	for _, bad := range []string{
		``, `{}`, `{"material_id":""}`, `{"material_id":"a/b"}`, `{"material_id":"m1","x":1}`,
		`{"transcript_id":"t1"}`, `{"material_id":"m1"} {}`, `{"material_id":1}`,
	} {
		if _, err := ParseMaterialPayload(bad); !errors.Is(err, ErrInvalidPostProcessRequest) {
			t.Errorf("ParseMaterialPayload(%q) = %v", bad, err)
		}
	}
	// A transcript payload is not a material payload, nor the reverse.
	if _, err := ParsePostProcessPayload(r.Payload()); err == nil {
		t.Fatal("a material payload read as a transcript one")
	}
	if MaterialJobKeyPrefix(MaterialSummaryJobType, "m1") != "material_summary:m1:" {
		t.Fatal("key prefix")
	}
}
