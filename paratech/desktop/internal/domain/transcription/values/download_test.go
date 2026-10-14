package values

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"huggingface.co", "huggingface.co", true},
		{"HuggingFace.CO", "huggingface.co", true},
		{"huggingface.co.", "huggingface.co", true},
		{"huggingface.co:443", "huggingface.co", true},
		{"huggingface.co.:443", "huggingface.co", true},
		{"huggingface.co:0443", "huggingface.co", true},
		{"example.com:8443", "example.com:8443", true},
		{"127.0.0.1:50123", "127.0.0.1:50123", true},
		{"[::1]:443", "[::1]", true},
		{"[FE80::1]:8443", "[fe80::1]:8443", true},
		{"xn--e1afmkfd.xn--p1ai", "xn--e1afmkfd.xn--p1ai", true},
		{"", "", false},
		{"host:", "", false},
		{"host:0", "", false},
		{"host:65536", "", false},
		{"host:abc", "", false},
		{"host:-1", "", false},
		{"a..b", "", false},
		{".", "", false},
		{"-a.com", "", false},
		{"a_b.com", "", false},
		{"пример.рф", "", false},
		{"user@host", "", false},
		{"[::1", "", false},
		{"[::1]x", "", false},
		{"[zz::1]", "", false},
		{"a:b:c", "", false},
		{"host%2ecom", "", false},
		{strings.Repeat("a", 300), "", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeHost(tc.in)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidHost) {
					t.Fatalf("NormalizeHost(%q) = %q, %v; want ErrInvalidHost", tc.in, got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("NormalizeHost(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestHostAllowlistCheck(t *testing.T) {
	a, err := NewHostAllowlist([]string{"huggingface.co", "CDN.example.com.", "127.0.0.1:8443"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.Hosts(), []string{"127.0.0.1:8443", "cdn.example.com", "huggingface.co"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Hosts() = %v, want %v", got, want)
	}

	tests := []struct {
		url  string
		want error
	}{
		{"https://huggingface.co/a.bin", nil},
		{"https://HUGGINGFACE.CO./a.bin", nil},
		{"https://huggingface.co:443/a.bin", nil},
		{"HTTPS://cdn.example.com/x?sig=1", nil},
		{"https://127.0.0.1:8443/a.bin", nil},
		{"https://127.0.0.1/a.bin", ErrHostNotAllowed},
		{"https://huggingface.co:8443/a.bin", ErrHostNotAllowed},
		{"https://evil.huggingface.co/a.bin", ErrHostNotAllowed},
		{"https://huggingface.co.evil.com/a.bin", ErrHostNotAllowed},
		{"https://169.254.169.254/latest", ErrHostNotAllowed},
		{"http://huggingface.co/a.bin", ErrInvalidSourceURL},
		{"ftp://huggingface.co/a.bin", ErrInvalidSourceURL},
		{"https://user:pw@huggingface.co/a.bin", ErrInvalidSourceURL},
		{"//huggingface.co/a.bin", ErrInvalidSourceURL},
		{"https:huggingface.co", ErrInvalidSourceURL},
		{"https:///a.bin", ErrInvalidHost},
		{"https://huggingface.co%2eevil.com/a", ErrInvalidSourceURL},
		{"%zz", ErrInvalidSourceURL},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			err := a.Check(tc.url)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Check(%q) = %v, want %v", tc.url, err, tc.want)
			}
		})
	}

	if err := (HostAllowlist{}).Check("https://huggingface.co/a.bin"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("zero allowlist allowed a host: %v", err)
	}
	if _, err := NewHostAllowlist([]string{"ok.com", "bad host"}); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("NewHostAllowlist accepted a bad host: %v", err)
	}
	if _, err := NewHostAllowlist(make([]string, MaxDownloadHosts+1)); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("NewHostAllowlist accepted too many hosts: %v", err)
	}
}

func TestCatalogDownloadHosts(t *testing.T) {
	cat, err := ParseCatalog([]byte(`{"schema_version": 1, "models": [` + entryJSON(nil) + `, ` + secondEntry() + `],
		"download_hosts": ["cas-bridge.xethub.hf.co", "CDN-LFS.huggingface.co."]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cas-bridge.xethub.hf.co", "cdn-lfs.huggingface.co", "huggingface.co"}
	if got := cat.DownloadHosts().Hosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DownloadHosts() = %v, want %v", got, want)
	}

	cat, err = ParseCatalog([]byte(manifest(entryJSON(nil))))
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.DownloadHosts().Hosts(); !reflect.DeepEqual(got, []string{"huggingface.co"}) {
		t.Fatalf("DownloadHosts() without download_hosts = %v", got)
	}

	bad := []struct{ name, doc string }{
		{"bad host", `{"schema_version": 1, "models": [` + entryJSON(nil) + `], "download_hosts": ["a b"]}`},
		{"not a list", `{"schema_version": 1, "models": [` + entryJSON(nil) + `], "download_hosts": "a.com"}`},
		{"repeated key", `{"schema_version": 1, "models": [` + entryJSON(nil) + `], "download_hosts": [], "download_hosts": []}`},
		{"bad source host", manifest(entryJSON(map[string]string{"source_url": `"https://a_b.com/m.bin"`}))},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseCatalog([]byte(tc.doc)); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("ParseCatalog = %v, want ErrInvalidCatalog", err)
			}
		})
	}
}

func testEntry(t *testing.T, size int64) CatalogEntry {
	t.Helper()
	src, err := ParseSourceURL("https://example.com/m.bin")
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewCatalogEntry(CatalogEntrySpec{
		ID: "m", Name: "m", Version: "1", SizeBytes: size, SHA256: sumA, SourceURL: src,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestResumeOffset(t *testing.T) {
	e := testEntry(t, 100)
	tests := []struct {
		have, offset int64
		complete     bool
	}{
		{0, 0, false},
		{40, 40, false},
		{100, 100, true},
		{101, 0, false},
		{-1, 0, false},
	}
	for _, tc := range tests {
		offset, complete := e.ResumeOffset(tc.have)
		if offset != tc.offset || complete != tc.complete {
			t.Errorf("ResumeOffset(%d) = %d, %v; want %d, %v", tc.have, offset, complete, tc.offset, tc.complete)
		}
	}
}

func TestPlanResume(t *testing.T) {
	e := testEntry(t, 100)
	tests := []struct {
		name    string
		offset  int64
		reply   RangeReply
		want    ResumeStep
		wantErr error
	}{
		{"206 continues", 40, RangeReply{Status: RangePartial, Start: 40, Total: 100}, StepAppend, nil},
		{"206 unknown total", 40, RangeReply{Status: RangePartial, Start: 40, Total: UnknownTotal}, StepAppend, nil},
		{"206 from zero", 0, RangeReply{Status: RangePartial, Start: 0, Total: 100}, StepAppend, nil},
		{"206 wrong start", 40, RangeReply{Status: RangePartial, Start: 30, Total: 100}, StepRestart, nil},
		{"206 wrong start from zero", 0, RangeReply{Status: RangePartial, Start: 10, Total: 100}, 0, ErrUnexpectedRange},
		{"206 other file", 40, RangeReply{Status: RangePartial, Start: 40, Total: 120}, 0, ErrSizeMismatch},
		{"200 ignores range", 40, RangeReply{Status: RangeFull, Total: 100}, StepReplace, nil},
		{"200 unknown length", 40, RangeReply{Status: RangeFull, Total: UnknownTotal}, StepReplace, nil},
		{"200 other file", 0, RangeReply{Status: RangeFull, Total: 99}, 0, ErrSizeMismatch},
		{"416 restarts", 40, RangeReply{Status: RangeNotSatisfiable, Total: UnknownTotal}, StepRestart, nil},
		{"416 other file", 40, RangeReply{Status: RangeNotSatisfiable, Total: 30}, 0, ErrSizeMismatch},
		{"416 from zero", 0, RangeReply{Status: RangeNotSatisfiable, Total: 100}, 0, ErrUnexpectedRange},
		{"unknown status", 40, RangeReply{Status: 0, Total: 100}, 0, ErrUnexpectedRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.PlanResume(tc.offset, tc.reply)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("PlanResume = %v, %v; want %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("PlanResume = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestCheckDiskSpace(t *testing.T) {
	e := testEntry(t, 1000)
	tests := []struct {
		name        string
		have, free  int64
		ok          bool
		wantMissing int64
	}{
		{"fresh, exactly enough", 0, 1000 + DiskSpaceMargin, true, 0},
		{"fresh, one byte short", 0, 999 + DiskSpaceMargin, false, 1000 + DiskSpaceMargin},
		{"resume needs only the rest", 600, 400 + DiskSpaceMargin, true, 0},
		{"too long partial counts as fresh", 2000, 400 + DiskSpaceMargin, false, 1000 + DiskSpaceMargin},
		{"complete needs only the margin", 1000, DiskSpaceMargin, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := e.CheckDiskSpace(tc.have, tc.free)
			if tc.ok {
				if err != nil {
					t.Fatalf("CheckDiskSpace = %v", err)
				}
				return
			}
			var ise *InsufficientSpaceError
			if !errors.As(err, &ise) || !errors.Is(err, ErrInsufficientSpace) {
				t.Fatalf("CheckDiskSpace = %v, want *InsufficientSpaceError", err)
			}
			if ise.Required != tc.wantMissing || ise.Available != tc.free {
				t.Fatalf("error = %+v, want required %d available %d", ise, tc.wantMissing, tc.free)
			}
		})
	}
}
