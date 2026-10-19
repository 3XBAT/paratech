package prompts

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

// TestSchemasMatchTheParsers keeps the schema given to the model and the parser in step: every object is closed and
// requires exactly the fields of its wire struct, types agree, and every length and count limit of the schema is the
// constant the parser enforces (and no limit is missing from the schema).
func TestSchemasMatchTheParsers(t *testing.T) {
	cases := []struct {
		op     values.Operation
		wire   any
		limits map[string]float64
	}{
		{values.OperationSummary, summaryWire{}, map[string]float64{
			"summary.minLength": 1, "summary.maxLength": MaxSummaryRunes,
			"key_points.maxItems": MaxKeyPoints, "key_points[].minLength": 1, "key_points[].maxLength": MaxKeyPointRunes,
		}},
		{values.OperationExtractHomework, extractWire{}, map[string]float64{
			"assignments.maxItems":                MaxHomework,
			"assignments[].title.minLength":       1,
			"assignments[].title.maxLength":       MaxTitleRunes,
			"assignments[].description.maxLength": MaxDescriptionRunes,
			"subject_hint.maxLength":              MaxHintRunes,
			"teacher_hint.maxLength":              MaxHintRunes,
		}},
		{values.OperationHomeworkDraft, draftWire{}, map[string]float64{
			"title.minLength": 1, "title.maxLength": MaxTitleRunes, "body.minLength": 1, "body.maxLength": MaxDraftBodyRunes,
		}},
	}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal([]byte(Schema(tc.op)), &schema); err != nil {
				t.Fatalf("schema is not JSON: %v", err)
			}
			if schema["title"] != string(tc.op) {
				t.Fatalf("schema title = %v", schema["title"])
			}

			found := map[string]float64{}
			walk(t, schema, reflect.TypeOf(tc.wire), "", found)
			if !maps.Equal(found, tc.limits) {
				t.Fatalf("schema limits = %v\nparser limits = %v", found, tc.limits)
			}
			if !strings.Contains(Instructions(tc.op), Schema(tc.op)) {
				t.Fatalf("the instructions do not carry the schema")
			}
		})
	}
}

func walk(t *testing.T, node map[string]any, typ reflect.Type, path string, found map[string]float64) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for _, k := range []string{"minLength", "maxLength", "maxItems"} {
		if v, ok := node[k].(float64); ok {
			found[strings.TrimPrefix(path, ".")+"."+k] = v
		}
	}

	switch typ {
	case reflect.TypeOf(nullable{}):
		if !reflect.DeepEqual(node["type"], []any{"string", "null"}) {
			t.Fatalf("%s: type %v, the parser takes a string or null", path, node["type"])
		}
		if node["format"] != "date-time" {
			t.Fatalf("%s: format %v, the parser takes an RFC 3339 time", path, node["format"])
		}
		return
	case reflect.TypeOf(""):
		if node["type"] != "string" {
			t.Fatalf("%s: type %v, the parser takes a string", path, node["type"])
		}
		return
	}

	switch typ.Kind() {
	case reflect.Slice:
		if node["type"] != "array" {
			t.Fatalf("%s: type %v, the parser takes an array", path, node["type"])
		}
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array without items", path)
		}
		walk(t, items, typ.Elem(), path+"[]", found)

	case reflect.Struct:
		if node["type"] != "object" || node["additionalProperties"] != false {
			t.Fatalf("%s: not a closed object", path)
		}
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object without properties", path)
		}
		var required []string
		for _, r := range node["required"].([]any) {
			required = append(required, r.(string))
		}
		fields := map[string]reflect.Type{}
		for i := range typ.NumField() {
			f := typ.Field(i)
			fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f.Type
		}

		want := slices.Sorted(maps.Keys(fields))
		if got := slices.Sorted(maps.Keys(props)); !slices.Equal(got, want) {
			t.Fatalf("%s: schema properties %v, parser fields %v", path, got, want)
		}
		if slices.Sort(required); !slices.Equal(required, want) {
			t.Fatalf("%s: schema requires %v, the parser requires %v", path, required, want)
		}
		for name, ft := range fields {
			walk(t, props[name].(map[string]any), ft, path+"."+name, found)
		}

	default:
		t.Fatalf("%s: unexpected parser type %v", path, typ)
	}
}
