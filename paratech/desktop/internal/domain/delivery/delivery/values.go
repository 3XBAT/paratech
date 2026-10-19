package delivery

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Bounds of the fields of a delivery.
const (
	MaxRecipientLen    = 254
	MaxSubjectRunes    = 200
	MaxMessageIDLen    = 250
	MaxKeyBytes        = 256
	MaxLastErrorBytes  = 512
	sha256HexLen       = 64
	minRecipientLength = 3
)

// Attachment is the file a delivery sends and the content the user confirmed: its SHA-256 and size. The adapter
// sends the file only if the bytes it read match (TOCTOU between confirmation and sending).
type Attachment struct {
	ref    shared.ArtifactRef
	sha256 string
	size   int64
}

// NewAttachment validates the reference, a lowercase hex SHA-256 and a non-negative size.
func NewAttachment(ref shared.ArtifactRef, sha256Hex string, size int64) (Attachment, error) {
	if ref.IsZero() {
		return Attachment{}, fmt.Errorf("%w: no attachment", ErrInvalidDelivery)
	}
	if len(sha256Hex) != sha256HexLen || size < 0 || strings.ContainsFunc(sha256Hex, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	}) {
		return Attachment{}, fmt.Errorf("%w: attachment digest must be 64 lowercase hex characters and a non-negative size",
			ErrInvalidDelivery)
	}
	return Attachment{ref: ref, sha256: sha256Hex, size: size}, nil
}

// Ref returns the file to attach.
func (a Attachment) Ref() shared.ArtifactRef { return a.ref }

// SHA256 returns the lowercase hex SHA-256 of the confirmed content.
func (a Attachment) SHA256() string { return a.sha256 }

// Size returns the size of the confirmed content in bytes.
func (a Attachment) Size() int64 { return a.size }

// Media types of an attachment, by the extension of its file.
const (
	MediaTypeText = "text/plain"
	MediaTypeDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// MediaType returns the MIME type the email declares for the attachment: a Word file (.docx, TASK-094) or plain text
// (the text export and anything else).
func (a Attachment) MediaType() string {
	if strings.HasSuffix(strings.ToLower(a.ref.Path()), ".docx") {
		return MediaTypeDocx
	}
	return MediaTypeText
}

// Matches reports whether content with the given SHA-256 and size is the confirmed one.
func (a Attachment) Matches(sha256Hex string, size int64) bool {
	return a.sha256 == sha256Hex && a.size == size
}

// checkRecipient accepts a plain address "local@domain" without display name, whitespace, control characters or any
// character that could turn it into several addresses or a header. It repeats the rule of the trusted recipient of the
// coursework context (contexts do not import each other, ARCH-11), so a tampered row cannot inject a header.
func checkRecipient(address string) error {
	if len(address) < minRecipientLength || len(address) > MaxRecipientLen {
		return fmt.Errorf("%w: recipient length must be %d-%d", ErrInvalidDelivery, minRecipientLength, MaxRecipientLen)
	}
	if strings.ContainsFunc(address, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune(`<>(),;:"[]\`, r)
	}) {
		return fmt.Errorf("%w: recipient has a forbidden character", ErrInvalidDelivery)
	}
	local, domain, ok := strings.Cut(address, "@")
	if !ok || local == "" || strings.Contains(domain, "@") || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return fmt.Errorf("%w: recipient is not local@domain", ErrInvalidDelivery)
	}
	return nil
}

// checkSubject accepts 1 to MaxSubjectRunes characters of valid UTF-8 without control characters (no CR or LF, so the
// subject cannot start another header).
func checkSubject(s string) error {
	n := utf8.RuneCountInString(s)
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || n > MaxSubjectRunes || strings.ContainsFunc(s, unicode.IsControl) {
		return fmt.Errorf("%w: subject must be one line of 1-%d characters", ErrInvalidDelivery, MaxSubjectRunes)
	}
	return nil
}

// checkMessageID accepts an RFC 5322 msg-id in the simple form "<left@right>": printable ASCII without spaces, one @,
// no further angle brackets.
func checkMessageID(id string) error {
	inner, ok := strings.CutPrefix(id, "<")
	if ok {
		inner, ok = strings.CutSuffix(inner, ">")
	}
	if !ok || len(id) > MaxMessageIDLen || strings.ContainsFunc(inner, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' || r == '<' || r == '>'
	}) {
		return fmt.Errorf("%w: message id must be <left@right> of printable ASCII", ErrInvalidDelivery)
	}
	left, right, ok := strings.Cut(inner, "@")
	if !ok || left == "" || right == "" || strings.Contains(right, "@") {
		return fmt.Errorf("%w: message id must be <left@right>", ErrInvalidDelivery)
	}
	return nil
}

// checkKey accepts 1 to MaxKeyBytes printable ASCII characters without spaces.
func checkKey(key string) error {
	if key == "" || len(key) > MaxKeyBytes || strings.ContainsFunc(key, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' '
	}) {
		return fmt.Errorf("%w: idempotency key must be 1-%d printable ASCII characters", ErrInvalidDelivery, MaxKeyBytes)
	}
	return nil
}

// limitError keeps a last error short and on one line; it is stored and shown, and the adapters give fixed texts.
func limitError(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) <= MaxLastErrorBytes {
		return s
	}
	cut := MaxLastErrorBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
