package random

import (
	"bytes"
	"testing"
)

func TestFill(t *testing.T) {
	a, b := make([]byte, 32), make([]byte, 32)
	if err := (Source{}).Fill(a); err != nil {
		t.Fatal(err)
	}
	if err := (Source{}).Fill(b); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, make([]byte, 32)) || bytes.Equal(a, b) {
		t.Fatal("Fill does not produce random bytes")
	}
}
