//go:build windows

package wincred

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"paratech/desktop/internal/domain/shared"
)

// The Go struct must match CREDENTIALW byte for byte.
func TestCredentialLayout(t *testing.T) {
	var c credential
	ptr := unsafe.Sizeof(uintptr(0))
	want64 := map[string]uintptr{"size": 80, "TargetName": 8, "LastWritten": 24, "CredentialBlobSize": 32, "CredentialBlob": 40, "Persist": 48, "Attributes": 56, "UserName": 72}
	want32 := map[string]uintptr{"size": 52, "TargetName": 8, "LastWritten": 16, "CredentialBlobSize": 24, "CredentialBlob": 28, "Persist": 32, "Attributes": 40, "UserName": 48}
	want := want64
	if ptr == 4 {
		want = want32
	}

	got := map[string]uintptr{
		"size":               unsafe.Sizeof(c),
		"TargetName":         unsafe.Offsetof(c.TargetName),
		"LastWritten":        unsafe.Offsetof(c.LastWritten),
		"CredentialBlobSize": unsafe.Offsetof(c.CredentialBlobSize),
		"CredentialBlob":     unsafe.Offsetof(c.CredentialBlob),
		"Persist":            unsafe.Offsetof(c.Persist),
		"Attributes":         unsafe.Offsetof(c.Attributes),
		"UserName":           unsafe.Offsetof(c.UserName),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %d, want %d", k, got[k], w)
		}
	}
}

func TestMapErrno(t *testing.T) {
	cases := []struct {
		desc string
		err  error
		is   error
	}{
		{"not found", windows.ERROR_NOT_FOUND, shared.ErrNotFound},
		{"access denied", windows.ERROR_ACCESS_DENIED, shared.ErrStoragePermission},
		{"no logon session", windows.ERROR_NO_SUCH_LOGON_SESSION, errNoSession},
		{"other errno", windows.ERROR_INVALID_FLAGS, shared.ErrStorage},
		{"not an errno", errors.New("odd"), shared.ErrStorage},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mapErrno("read", tc.err)
			if !errors.Is(got, tc.is) {
				t.Fatalf("mapErrno = %v, want %v", got, tc.is)
			}
			var errno syscall.Errno
			if errors.As(got, &errno) {
				t.Fatalf("mapErrno leaks the raw errno: %v", got)
			}
		})
	}
	if got := mapErrno("write", windows.ERROR_INVALID_FLAGS); !strings.Contains(got.Error(), "code 1004") {
		t.Errorf("unknown errors keep the code for diagnosis: %v", got)
	}
}

// TestRealCredentialManager writes to the Credential Manager of the current user under a unique test service.
func TestRealCredentialManager(t *testing.T) {
	ctx := context.Background()
	service := "test-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	s, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Set(ctx, "token", "first"); err != nil {
		if errors.Is(err, errNoSession) {
			t.Skipf("no credential set in this logon session: %v", err)
		}
		t.Fatal(err)
	}

	// Registered after the first write succeeded; it runs even when a later check fails.
	t.Cleanup(func() {
		for _, n := range []string{"token", "big", "unicode"} {
			if err := s.Delete(ctx, n); err != nil {
				t.Errorf("cleanup Delete(%s): %v", n, err)
			}
		}
	})

	// The entry is a generic credential under the documented target name.
	raw, err := winBackend{}.read(TargetPrefix + service + "/token")
	if err != nil || string(raw) != "first" {
		t.Fatalf("raw read = %q, %v", raw, err)
	}

	if err := s.Set(ctx, "token", "second"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(ctx, "token"); err != nil || got != "second" {
		t.Fatalf("Get after overwrite = %q, %v", got, err)
	}

	big := strings.Repeat("b", MaxSecretBytes)
	if err := s.Set(ctx, "big", big); err != nil {
		t.Fatalf("Set at the size limit: %v", err)
	}
	if got, _ := s.Get(ctx, "big"); got != big {
		t.Fatalf("Get at the size limit returned %d bytes", len(got))
	}
	if err := s.Set(ctx, "big", big+"x"); !errors.Is(err, ErrSecretTooLarge) {
		t.Fatalf("Set over the limit = %v, want ErrSecretTooLarge", err)
	}

	uni := "пароль-密码-🔑"
	if err := s.Set(ctx, "unicode", uni); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, "unicode"); got != uni {
		t.Fatalf("unicode round trip = %q", got)
	}

	if err := s.Delete(ctx, "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "token"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists(ctx, "token"); err != nil || ok {
		t.Fatalf("Exists after Delete = %v, %v", ok, err)
	}
	if err := s.Delete(ctx, "token"); err != nil {
		t.Fatalf("second Delete = %v, want nil", err)
	}

	if msg := fmt.Sprint(s.Get(ctx, "token")); strings.Contains(msg, service) {
		t.Errorf("error reveals the target name: %s", msg)
	}
}
