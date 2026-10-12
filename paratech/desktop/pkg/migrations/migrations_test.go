package migrations

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var fileName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

func TestFS(t *testing.T) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded migrations")
	}
	prev := 0
	for _, e := range entries { // ReadDir sorts by file name, so versions must be consecutive from 1
		m := fileName.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("%s: want NNNN_snake_case.sql", e.Name())
			continue
		}
		version, _ := strconv.Atoi(m[1])
		if version != prev+1 {
			t.Errorf("%s: version %d follows %d, want consecutive versions", e.Name(), version, prev)
		}
		prev = version

		data, err := fs.ReadFile(FS(), e.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		up, down := strings.Index(text, "-- +goose Up"), strings.Index(text, "-- +goose Down")
		if up < 0 || down < 0 || up > down {
			t.Errorf("%s: want a goose Up marker followed by a Down marker", e.Name())
		}
	}
}
