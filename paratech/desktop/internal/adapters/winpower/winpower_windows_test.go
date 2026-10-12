//go:build windows

package winpower

import "testing"

func TestReadACPowerReal(t *testing.T) {
	if _, err := readACPower(); err != nil {
		t.Fatalf("GetSystemPowerStatus: %v", err)
	}
}
