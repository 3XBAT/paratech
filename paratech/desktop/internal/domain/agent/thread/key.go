package thread

import (
	"fmt"

	"paratech/desktop/internal/domain/agent/values"
)

// Key identifies the thread of a subject: the subject ID, or NoSubject («Без предмета»). One thread per key.
type Key string

// NoSubject is the key of the thread without a subject.
const NoSubject Key = "NONE"

// SubjectKey returns the key of a subject's thread.
func SubjectKey(subject values.Ref) (Key, error) {
	if subject.Kind() != values.KindSubject {
		return "", fmt.Errorf("%w: thread key needs a subject id", ErrInvalidThread)
	}
	if Key(subject.ID()) == NoSubject {
		return "", fmt.Errorf("%w: subject id collides with the no-subject key", ErrInvalidThread)
	}
	return Key(subject.ID()), nil
}

// KeyOf returns the key of the thread a scope belongs to (the context chip picks the thread).
func KeyOf(scope values.Scope) (Key, error) {
	if scope.Subject().IsZero() {
		return NoSubject, nil
	}
	return SubjectKey(scope.Subject())
}

// ParseKey checks a stored key.
func ParseKey(s string) (Key, error) {
	if Key(s) == NoSubject {
		return NoSubject, nil
	}
	r, err := values.NewRef(values.KindSubject, s)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidThread, err)
	}
	return SubjectKey(r)
}

// Subject returns the subject of the key; false for NoSubject.
func (k Key) Subject() (values.Ref, bool) {
	if k == NoSubject || k == "" {
		return values.Ref{}, false
	}
	r, err := values.NewRef(values.KindSubject, string(k))
	return r, err == nil
}

// String returns the stored form.
func (k Key) String() string { return string(k) }
