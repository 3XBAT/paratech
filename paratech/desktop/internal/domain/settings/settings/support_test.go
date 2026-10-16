package settings

import (
	"strings"
	"testing"
	"time"
)

func TestSupportSummary(t *testing.T) {
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	s := New()
	for _, err := range []error{
		s.SetHomeworkRecipient("teacher@school.example", now), s.SetSMTPHost("smtp.mail.example", now),
		s.SetSMTPPort(465, now), s.SetSMTPSecurity(MailSecurityTLS, now), s.SetSMTPLogin("student.ivanov@mail.example", now),
		s.SetHubURL("https://hub.example.org:8443/tenant-ivanov/v1", now), s.SetRecordingSource("chrome.exe", now),
		s.SetProcessOnlyOnACPower(false, now), s.SelectDefaultSttModel("ggml-base", now),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	got := s.SupportSummary()
	want := Summary{
		RecordingPromptsEnabled: true, ProcessOnlyOnACPower: false,
		DefaultSttModel: "ggml-base", RecordingSource: "chrome.exe",
		HomeworkRecipient: "***@school.example", SMTPHost: "smtp.mail.example", SMTPPort: 465, SMTPSecurity: MailSecurityTLS,
		SMTPLogin: "***@mail.example", MailConfigured: true, HubURL: "https://hub.example.org:8443",
	}
	if got != want {
		t.Fatalf("SupportSummary = %+v\nwant %+v", got, want)
	}
	for _, private := range []string{"teacher", "student", "ivanov", "tenant"} {
		if strings.Contains(strings.ToLower(fmtSummary(got)), private) {
			t.Errorf("the summary contains %q", private)
		}
	}
}

func TestSupportSummaryOfDefaults(t *testing.T) {
	got := New().SupportSummary()
	if got.HomeworkRecipient != "" || got.SMTPLogin != "" || got.HubURL != "" || got.MailConfigured || !got.RecordingPromptsEnabled {
		t.Fatalf("SupportSummary of the defaults = %+v", got)
	}
}

func TestMaskAddress(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "a@b.example": "***@b.example", "student-login": "***", "x@y": "***",
	} {
		if got := maskAddress(in); got != want {
			t.Errorf("maskAddress(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"": "", "http://localhost:8080/api": "http://localhost:8080", "https://hub.example": "https://hub.example", "::": "***",
	} {
		if got := hubOrigin(in); got != want {
			t.Errorf("hubOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

func fmtSummary(s Summary) string {
	return strings.Join([]string{string(s.DefaultSttModel), string(s.RecordingSource), s.HomeworkRecipient, s.SMTPHost,
		string(s.SMTPSecurity), s.SMTPLogin, s.HubURL}, " ")
}
