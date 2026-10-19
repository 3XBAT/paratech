package smtp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// TASK-094: the attachment is declared with the media type of its file: a Word file as .docx, the text export as
// plain text, as before.
func TestAttachmentContentType(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"a1/homework.md", `Content-Type: text/plain; name="homework.md"`},
		{"a1/homework.docx", `Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document; name="homework.docx"`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			f := newFixture(t, testenv.SMTPBehavior{}, nil)
			body := "PK fake docx bytes"
			if err := f.mailer.Send(context.Background(), deliveryOf(t, tc.path, body), strings.NewReader(body)); err != nil {
				t.Fatalf("Send: %v", err)
			}
			msgs := f.srv.Messages()
			if len(msgs) != 1 {
				t.Fatalf("server received %d messages", len(msgs))
			}
			raw := string(msgs[0])
			name := tc.path[strings.LastIndex(tc.path, "/")+1:]
			if !strings.Contains(raw, tc.want) || !strings.Contains(raw, `filename="`+name+`"`) {
				t.Fatalf("message lacks %q:\n%s", tc.want, raw)
			}
		})
	}
}

func deliveryOf(t *testing.T, path, body string) *delivery.Delivery {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactExports, path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	att, err := delivery.NewAttachment(ref, hex.EncodeToString(sum[:]), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	d, err := delivery.New(delivery.Spec{
		ID: "d1", AssignmentID: "a1", Recipient: "teacher@school.example", Subject: "Домашнее задание", Attachment: att,
		MessageID: "<m1.d1@paratech.test>", Key: "proposal:p1", ConfirmedAt: now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
