//go:build windows

package winpower

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows has no wrapper for this call, so it is loaded lazily from kernel32.
var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// systemPowerStatus mirrors SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func readACPower() (bool, error) {
	var s systemPowerStatus
	r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))) //nolint:gosec // syscall needs the struct address
	if r == 0 {
		return false, fmt.Errorf("%w: GetSystemPowerStatus: %w", ErrQuery, err)
	}
	return onAC(s.ACLineStatus), nil
}
