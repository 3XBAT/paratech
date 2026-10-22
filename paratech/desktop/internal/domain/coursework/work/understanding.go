package work

import (
	"fmt"
	"slices"
	"strings"
)

// Field is a field of «Как я понял задание» the student can edit.
type Field string

// Fields of the card; the requirement fields are named as their wire keys.
const (
	FieldWorkType   Field = "work_type"
	FieldVolume     Field = "volume"
	FieldFileFormat Field = "file_format"
	FieldFormatting Field = "formatting"
	FieldVariant    Field = "variant"
	FieldDeadline   Field = "deadline"
	FieldSources    Field = "sources"
)

// Fields returns every field, in card order.
func Fields() []Field {
	return []Field{FieldWorkType, FieldVolume, FieldFileFormat, FieldFormatting, FieldVariant, FieldDeadline, FieldSources}
}

// Valid reports whether f is one of the fields.
func (f Field) Valid() bool { return slices.Contains(Fields(), f) }

// Understanding is «Как я понял задание»: the type of the work and its requirements.
type Understanding struct {
	WorkType     WorkType
	Requirements Requirements
}

// get returns the value of field f.
func (u Understanding) get(f Field) string {
	if f == FieldWorkType {
		return string(u.WorkType)
	}
	return *u.Requirements.fields()[string(f)]
}

// set sets the value of field f.
func (u *Understanding) set(f Field, v string) {
	if f == FieldWorkType {
		u.WorkType = WorkType(v)
		return
	}
	*u.Requirements.fields()[string(f)] = v
}

// validEdit reports whether v is a value the student may give field f: a work type of the closed set, or one line
// of at most MaxRequirementRunes characters (empty: unknown).
func validEdit(f Field, v string) bool {
	if f == FieldWorkType {
		return WorkType(v).Valid()
	}
	return f.Valid() && v == strings.TrimSpace(v) && boundedText(v, MaxRequirementRunes, false, true)
}

// cleanEdits trims the student's card and returns the fields that differ from the reader's understanding: equal
// values are no edit, so a field set back to the reader's value follows the reader again. Error: ErrInvalidReply.
func cleanEdits(reader, student Understanding) (map[Field]string, error) {
	out := map[Field]string{}
	for _, f := range Fields() {
		v := strings.TrimSpace(student.get(f))
		if !validEdit(f, v) {
			return nil, fmt.Errorf("%w: field %s", ErrInvalidReply, f)
		}
		if v != reader.get(f) {
			out[f] = v
		}
	}
	return out, nil
}
