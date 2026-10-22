package strictjson_test

import (
	"encoding/json"
	"errors"
	"testing"

	"paratech/hub/internal/domain/llm/services/strictjson"
)

func TestDecodeObjectIsStrict(t *testing.T) {
	read := func(data string) (map[string]string, error) {
		got := map[string]string{}
		err := strictjson.DecodeObject(data, func(key string, d *json.Decoder) error {
			raw, _, ok := strictjson.ReadRaw(d)
			if !ok {
				return strictjson.ErrShape
			}
			got[key] = raw
			return nil
		})
		return got, err
	}
	got, err := read(` {"a":1,"A":{"b":[1,2]},"n":null} `)
	if err != nil || got["a"] != "1" || got["A"] != `{"b":[1,2]}` || got["n"] != "null" {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, data := range []string{"", "[]", `{"a":1,"a":2}`, `{"a":1}{}`, `{"a":1} x`, `{"a":}`, "null"} {
		if _, err := read(data); !errors.Is(err, strictjson.ErrShape) {
			t.Errorf("%q: err = %v, want ErrShape", data, err)
		}
	}
}

func TestReadIntAndUnfence(t *testing.T) {
	for data, want := range map[string]bool{`{"n":3}`: true, `{"n":3.0}`: false, `{"n":"3"}`: false, `{"n":1e2}`: false} {
		err := strictjson.DecodeObject(data, func(_ string, d *json.Decoder) error {
			if _, ok := strictjson.ReadInt(d); !ok {
				return strictjson.ErrShape
			}
			return nil
		})
		if (err == nil) != want {
			t.Errorf("%s: err = %v", data, err)
		}
	}
	if s, ok := strictjson.Unfence("```json\n{}\n```"); !ok || s != "{}" {
		t.Fatalf("Unfence = %q, %v", s, ok)
	}
	if _, ok := strictjson.Unfence("\xff"); ok {
		t.Fatal("invalid UTF-8 accepted")
	}
}
