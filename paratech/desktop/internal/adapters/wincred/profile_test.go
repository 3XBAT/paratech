package wincred

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestCheckProfile(t *testing.T) {
	cases := []struct {
		desc    string
		profile Profile
		ok      bool
	}{
		{"default", DefaultProfile, true},
		{"hex", "0123456789abcdef", true},
		{"upper case", "0123456789ABCDEF", false},
		{"short", "0123456789abcde", false},
		{"long", "0123456789abcdef0", false},
		{"not hex", "0123456789abcdeg", false},
		{"separator", "01234567/9abcdef", false},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			err := CheckProfile(c.profile)
			if (err == nil) != c.ok {
				t.Fatalf("CheckProfile(%q) = %v, want ok=%v", c.profile, err, c.ok)
			}
			if err != nil && !errors.Is(err, shared.ErrInvalidID) {
				t.Fatalf("error %v must wrap shared.ErrInvalidID", err)
			}
			if _, err := newProfileStore("mail", c.profile, newFakeBackend()); (err == nil) != c.ok {
				t.Fatalf("newProfileStore = %v, want ok=%v", err, c.ok)
			}
		})
	}
}

// targetOf writes one secret through a store of profile and returns the only target name the backend saw.
func targetOf(t *testing.T, profile Profile, service, name string) string {
	t.Helper()
	fb := newFakeBackend()
	s, err := newProfileStore(service, profile, fb)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(context.Background(), name, "x"); err != nil {
		t.Fatal(err)
	}
	for target := range fb.data {
		return target
	}
	t.Fatal("nothing written")
	return ""
}

func TestProfileTargetNames(t *testing.T) {
	const p Profile = "0123456789abcdef"
	cases := []struct {
		profile Profile
		service string
		name    string
		want    string
	}{
		{DefaultProfile, "google", "refresh-token", "paratech/google/refresh-token"},
		{DefaultProfile, "hub", "api-key", "paratech/hub/api-key"},
		{p, "google", "refresh-token", "paratech-0123456789abcdef/google/refresh-token"},
		{p, "mail", "password", "paratech-0123456789abcdef/mail/password"},
	}
	for _, c := range cases {
		if got := targetOf(t, c.profile, c.service, c.name); got != c.want {
			t.Errorf("target(%q, %s, %s) = %q, want %q", c.profile, c.service, c.name, got, c.want)
		}
	}
}

// No target of one profile equals or starts like a target prefix of another, so neither can reach the other's
// entries, also with the case-insensitive names of Credential Manager.
func TestProfileTargetsNeverCollide(t *testing.T) {
	profiles := []Profile{DefaultProfile, "0123456789abcdef", "0123456789abcdee", "fedcba9876543210"}
	services := []string{"hub", "mail", "google", "google-x"}

	owner := map[string]Profile{}
	for _, p := range profiles {
		for _, svc := range services {
			s, err := newProfileStore(svc, p, newFakeBackend())
			if err != nil {
				t.Fatal(err)
			}
			for _, other := range profiles {
				if other == p {
					continue
				}
				o, _ := newProfileStore(svc, other, newFakeBackend())
				if strings.HasPrefix(strings.ToLower(s.prefix), strings.ToLower(o.prefix)) {
					t.Errorf("prefix %q of %q starts with prefix %q of %q", s.prefix, p, o.prefix, other)
				}
			}
			if prev, ok := owner[strings.ToLower(s.prefix)]; ok && prev != p {
				t.Errorf("prefix %q shared by %q and %q", s.prefix, prev, p)
			}
			owner[strings.ToLower(s.prefix)] = p
		}
	}
}

// The longest target name stays within CRED_MAX_GENERIC_TARGET_NAME_LENGTH (32767).
func TestProfileTargetLength(t *testing.T) {
	got := targetOf(t, "0123456789abcdef", strings.Repeat("s", MaxServiceLen), strings.Repeat("n", MaxNameLen))
	if len(got) > 32767 {
		t.Fatalf("target of %d chars", len(got))
	}
}

func TestProfileFor(t *testing.T) {
	base := t.TempDir()
	def := filepath.Join(base, "LearningAssistant")
	other := filepath.Join(base, "smoke-data")
	for _, d := range []string{def, other} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(base, "missing")

	isolated := ProfileFor(other, def)
	cases := []struct {
		desc       string
		dataDir    string
		defaultDir string
		want       Profile
	}{
		{"default dir", def, def, DefaultProfile},
		{"default dir with a trailing separator", def + string(filepath.Separator), def, DefaultProfile},
		{"default dir spelled with ..", filepath.Join(other, "..", "LearningAssistant"), def, DefaultProfile},
		{"other dir", other, def, isolated},
		{"other dir with a trailing separator", other + string(filepath.Separator), def, isolated},
		{"default unknown", def, "", ProfileFor(def, other)},
		{"default dir missing", def, missing, ProfileFor(def, other)},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			if got := ProfileFor(c.dataDir, c.defaultDir); got != c.want {
				t.Fatalf("ProfileFor(%q, %q) = %q, want %q", c.dataDir, c.defaultDir, got, c.want)
			}
		})
	}

	if !isolated.Isolated() || CheckProfile(isolated) != nil {
		t.Fatalf("profile of another dir = %q, want a valid isolated profile", isolated)
	}
	if ProfileFor(def, other) == isolated {
		t.Fatal("two directories share a profile")
	}
}

// A data dir that cannot be resolved still gets an isolated profile, never the default one (fail closed).
func TestProfileForFailsClosed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	cases := []struct {
		desc       string
		dataDir    string
		defaultDir string
	}{
		{"both missing", missing, missing},
		{"data dir missing", missing, t.TempDir()},
		{"no default", t.TempDir(), ""},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := ProfileFor(c.dataDir, c.defaultDir)
			if !got.Isolated() || CheckProfile(got) != nil {
				t.Fatalf("ProfileFor = %q, want a valid isolated profile", got)
			}
		})
	}
}

// A symlink to the default dir is the default dir. Creating one may need a privilege on Windows.
func TestProfileForFollowsSymlink(t *testing.T) {
	base := t.TempDir()
	def := filepath.Join(base, "real")
	if err := os.Mkdir(def, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(def, link); err != nil {
		t.Skipf("the OS refuses a symlink here: %v", err)
	}

	if got := ProfileFor(link, def); got != DefaultProfile {
		t.Fatalf("ProfileFor(link, default) = %q, want the default profile", got)
	}
	if got, want := ProfileFor(def, ""), ProfileFor(link, ""); got != want {
		t.Fatalf("profiles of a dir and its link differ: %q, %q", got, want)
	}
}
