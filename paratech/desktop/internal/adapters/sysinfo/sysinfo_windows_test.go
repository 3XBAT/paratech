//go:build windows

package sysinfo

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	if got := clean("Windows\x00 10\nPro"); got != "Windows 10Pro" {
		t.Fatalf("clean = %q", got)
	}
	if got := clean(strings.Repeat("я", 200)); len([]rune(got)) != maxText {
		t.Fatalf("clean kept %d runes", len([]rune(got)))
	}
}
