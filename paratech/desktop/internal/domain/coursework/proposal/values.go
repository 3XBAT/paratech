package proposal

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"paratech/desktop/internal/domain/shared"
)

// ID identifies a proposal. It is produced by the IDGenerator port of the use case.
type ID string

const maxIDLen = 128

// String returns the identifier as a plain string.
func (id ID) String() string { return string(id) }

// Validate reports whether the identifier is 1 to 128 printable ASCII characters without spaces.
func (id ID) Validate() error {
	if id == "" || len(id) > maxIDLen {
		return fmt.Errorf("%w: length must be 1-%d", ErrInvalidID, maxIDLen)
	}
	for _, r := range id {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' {
			return fmt.Errorf("%w: only printable ASCII without spaces", ErrInvalidID)
		}
	}
	return nil
}

// Kind is what the proposal asks the app to do. The set is closed: anything else is malformed.
type Kind string

// Proposal kinds, one per side effect the app may perform on behalf of a proposal.
const (
	KindSendHomework   Kind = "send_homework_email"   // email the exported homework of an assignment (PRD section 10)
	KindExportHomework Kind = "export_homework"       // write the homework text of an assignment to its export file
	KindDeadlineEvent  Kind = "create_deadline_event" // put the deadline of an assignment into the app calendar (PRD section 11.3)

	// KindSendWork emails the finished file of a turnkey work (TASK-180). Only the core proposes it (NewWorkSend, on the
	// student's action); a model answer naming it is malformed.
	KindSendWork Kind = "send_work_email"
)

// Kinds returns the kinds a model answer may propose (New).
func Kinds() []Kind { return []Kind{KindSendHomework, KindExportHomework, KindDeadlineEvent} }

// AllKinds returns every kind, those of the core included, for the schema checks.
func AllKinds() []Kind { return append(Kinds(), KindSendWork) }

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return slices.Contains(AllKinds(), k) }

// fromModel reports whether a model answer may propose k.
func (k Kind) fromModel() bool { return slices.Contains(Kinds(), k) }

// NeedsRecipient reports whether the kind is checked against the configured recipient.
func (k Kind) NeedsRecipient() bool { return k == KindSendHomework || k == KindSendWork }

// NeedsAttachment reports whether the kind is checked against the content of the file it attaches: the export file of
// the assignment or the file of the work.
func (k Kind) NeedsAttachment() bool { return k == KindSendHomework || k == KindSendWork }

// NeedsSubject reports whether the kind is checked against the email subject the user confirmed.
func (k Kind) NeedsSubject() bool { return k == KindSendHomework || k == KindSendWork }

// NeedsAssignmentTitle reports whether the kind takes the stored title of the assignment (the heading of an export).
func (k Kind) NeedsAssignmentTitle() bool { return k == KindExportHomework }

// NeedsWorkFile reports whether the kind attaches the library file of a work and is checked against where it is filed.
func (k Kind) NeedsWorkFile() bool { return k == KindSendWork }

// allows reports whether a proposal of kind k may attach format f: the text or Word export for a homework send, the
// Word or PowerPoint file for the send of a work.
func (k Kind) allows(f Format) bool {
	switch k {
	case KindSendHomework:
		return f == FormatText || f == FormatDocx
	case KindSendWork:
		return f == FormatDocx || f == FormatPptx
	}
	return false
}

// RejectReason tells why a proposal was rejected at validation or went stale before execution. The set is closed and
// written by the core, never by the model.
type RejectReason string

// Reject reasons.
const (
	ReasonUnknownAssignment RejectReason = "unknown_assignment"        // the referenced assignment does not exist locally
	ReasonNoRecipient       RejectReason = "no_recipient_configured"   // no homework recipient in the local settings
	ReasonNoArtifact        RejectReason = "homework_artifact_missing" // nothing was exported for the assignment (yet)
	ReasonDueOutOfRange     RejectReason = "due_out_of_range"          // the deadline is in the past or too far ahead

	// Stale only: the trusted state the user confirmed changed before execution.
	ReasonRecipientChanged RejectReason = "recipient_changed"         // the configured recipient is not the confirmed one
	ReasonArtifactChanged  RejectReason = "homework_artifact_changed" // the export file is not the confirmed content
	ReasonSubjectChanged   RejectReason = "email_subject_changed"     // the email subject is not the one the user saw
	ReasonFormatChanged    RejectReason = "attachment_format_changed" // the attachment format is not the one the user saw

	// Stale only: the student deleted the assignment after the proposal (ADR-0029); a new one is unknown_assignment.
	ReasonAssignmentDeleted RejectReason = "assignment_deleted"

	// Send of a work, rejected or stale: its file is no longer in «ДЗ» (or in «Входящие» where a work without a
	// subject leaves it).
	ReasonFileMoved RejectReason = "work_file_moved"
)

// validFor reports whether r is a reason a proposal in status s may carry.
func (r RejectReason) validFor(s Status) bool {
	switch s {
	case StatusRejected:
		return r == ReasonUnknownAssignment || r == ReasonNoRecipient || r == ReasonNoArtifact || r == ReasonDueOutOfRange ||
			r == ReasonFileMoved
	case StatusStale:
		return r == ReasonUnknownAssignment || r == ReasonRecipientChanged || r == ReasonNoArtifact ||
			r == ReasonArtifactChanged || r == ReasonSubjectChanged || r == ReasonFormatChanged || r == ReasonFileMoved ||
			r == ReasonAssignmentDeleted
	}
	return false
}

// Recipient is an email address taken from trusted local settings, never from model output.
type Recipient struct {
	address string
}

const maxAddressLen = 254

// NewRecipient validates a plain address "local@domain": no display name, no whitespace or control characters and
// none of the characters that would let it become several addresses or a header (< > ( ) , ; : " [ ] \). Both parts
// are dot-atoms (no leading, trailing or double dot), as net/mail requires.
func NewRecipient(address string) (Recipient, error) {
	if len(address) < 3 || len(address) > maxAddressLen {
		return Recipient{}, fmt.Errorf("%w: length must be 3-%d", ErrInvalidRecipient, maxAddressLen)
	}
	if strings.ContainsFunc(address, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune(`<>(),;:"[]\`, r)
	}) {
		return Recipient{}, fmt.Errorf("%w: forbidden character", ErrInvalidRecipient)
	}
	local, domain, ok := strings.Cut(address, "@")
	if !ok || strings.Contains(domain, "@") {
		return Recipient{}, fmt.Errorf("%w: want exactly one @", ErrInvalidRecipient)
	}
	if !dotAtom(local) {
		return Recipient{}, fmt.Errorf("%w: malformed local part", ErrInvalidRecipient)
	}
	if !strings.Contains(domain, ".") || !dotAtom(domain) {
		return Recipient{}, fmt.Errorf("%w: malformed domain", ErrInvalidRecipient)
	}
	return Recipient{address: address}, nil
}

// dotAtom reports whether s is non-empty and has no leading, trailing or double dot.
func dotAtom(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".") && !strings.Contains(s, "..")
}

// Address returns the email address.
func (r Recipient) Address() string { return r.address }

// IsZero reports whether no recipient is set.
func (r Recipient) IsZero() bool { return r.address == "" }

// Digest pins the content of a file: its SHA-256 and size. The use case computes it; the domain only compares.
type Digest struct {
	sha256 string
	size   int64
}

// NewDigest validates a lowercase hex SHA-256 (64 characters) and a non-negative size.
func NewDigest(sha256Hex string, size int64) (Digest, error) {
	if len(sha256Hex) != 64 || size < 0 || strings.ContainsFunc(sha256Hex, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	}) {
		return Digest{}, fmt.Errorf("%w: want 64 lowercase hex characters and a non-negative size", ErrInvalidDigest)
	}
	return Digest{sha256: sha256Hex, size: size}, nil
}

// SHA256 returns the lowercase hex SHA-256 of the content.
func (d Digest) SHA256() string { return d.sha256 }

// Size returns the content size in bytes.
func (d Digest) Size() int64 { return d.size }

// IsZero reports whether no content is pinned (an empty file still has a digest).
func (d Digest) IsZero() bool { return d == Digest{} }

// Facts is the trusted local state a proposal is validated and, before execution, re-checked against. The use case
// reads it from local ports (the assignment repository, the settings, the Storage Manager), only what the kind needs
// (Kind.NeedsRecipient, Kind.NeedsAttachment, Kind.NeedsAssignmentTitle); nothing in it comes from the model.
type Facts struct {
	AssignmentExists bool      // the referenced assignment is in the local database
	AssignmentTitle  string    // its stored title, when it exists
	Recipient        Recipient // the configured homework recipient; zero if none
	Attachment       Digest    // the content of ExportRef of the assignment; zero if the file does not exist

	// AssignmentDeleted: the student deleted it (ADR-0029); AssignmentExists is false then. Validate treats it as
	// unknown, Recheck makes the proposal STALE with ReasonAssignmentDeleted.
	AssignmentDeleted bool

	// Send only, read before execution (Kind.NeedsSubject): the email subject the core builds now from trusted state
	// and the one the confirmation dialog showed, carried back by the user's confirmation. Recheck is STALE when they
	// differ, so the email never gets a subject the user did not see.
	Subject      string
	ShownSubject string

	// ShownFormat is the attachment format the confirmation dialog showed (send only, read before execution); Recheck
	// is STALE when it is not the format the proposal pinned.
	ShownFormat Format

	// Send of a work only (Kind.NeedsWorkFile): the stored library file the work points at now (zero: none; Attachment
	// is the digest of its content) and whether it is still filed where the work put it: in «ДЗ», or in «Входящие» for
	// a work whose subject is none or gone. AttachmentName is the library name of that file: Validate pins its CopyName,
	// so a rename in the library before «Да» does not change the name the teacher gets.
	AttachmentRef  shared.ArtifactRef
	Filed          bool
	AttachmentName string
}

// Format is the file a send attaches: the text export or the Word export of the assignment (TASK-094), or the Word or
// PowerPoint file of a work (TASK-180).
type Format string

// Attachment formats.
const (
	FormatText Format = "txt"  // the text export (ExportRef), as before TASK-094
	FormatDocx Format = "docx" // the Word export with the title page, or the paper of a work
	FormatPptx Format = "pptx" // the presentation of a work
)

// Valid reports whether f is a known format.
func (f Format) Valid() bool { return f == FormatText || f == FormatDocx || f == FormatPptx }

// File names of the exported homework inside exports/<assignment id>/, by format.
const (
	exportFile     = "homework.md"
	exportDocxFile = "homework.docx"
)

// ExportRef is the file the homework of an assignment is exported to and attached from as text. The core derives it;
// the model cannot choose a path. The id is a safe file-name segment (shared.AssignmentID.Validate), so the reference
// cannot leave the exports directory.
func ExportRef(a shared.AssignmentID) (shared.ArtifactRef, error) {
	return ExportRefOf(a, FormatText)
}

// ExportRefOf is the export file of the assignment in format f: ExportRef for FormatText, homework.docx next to it for
// FormatDocx. An unknown format wraps ErrInvalidProposal.
func ExportRefOf(a shared.AssignmentID, f Format) (shared.ArtifactRef, error) {
	if err := a.Validate(); err != nil {
		return shared.ArtifactRef{}, err
	}
	name := exportFile
	switch f {
	case FormatText:
	case FormatDocx:
		name = exportDocxFile
	default:
		return shared.ArtifactRef{}, fmt.Errorf("%w: unknown attachment format %q", ErrInvalidProposal, string(f))
	}
	return shared.NewArtifactRef(shared.ArtifactExports, a.String()+"/"+name)
}

// The copy of a work's file a send attaches lives in exports/<sentDir>/<proposal id>/<name>.<format>: the library
// keeps its files without an extension, and the teacher gets the file under its name.
const (
	sentDir          = "sent"
	copyFallbackName = "работа"
	maxCopyNameRunes = 64 // Word refuses long full paths, as for the copies of «Открыть в Word»
)

// storedFileRef is where the library keeps the content of digest d (ADR-0024): materials/<sha256>.
func storedFileRef(d Digest) (shared.ArtifactRef, error) {
	if d.IsZero() {
		return shared.ArtifactRef{}, fmt.Errorf("%w: no pinned content", ErrInvalidProposal)
	}
	return shared.NewArtifactRef(shared.ArtifactMaterials, d.SHA256())
}

// CopyName is the file name the copy of a work's file gets: the library name of the material without its extension,
// made a safe file name (shared.SafeFileName, at most maxCopyNameRunes characters), "работа" when nothing is left, and
// the extension of format f. The name is local data the student imported or the app made, never model text.
func CopyName(materialName string, f Format) string {
	if i := strings.LastIndexByte(materialName, '.'); i >= 0 {
		materialName = materialName[:i]
	}
	if r := []rune(materialName); len(r) > maxCopyNameRunes {
		materialName = string(r[:maxCopyNameRunes])
	}
	name := shared.SafeFileName(materialName)
	if strings.Trim(name, "_ ") == "" {
		name = copyFallbackName
	}
	return name + "." + string(f)
}

// validCopyName reports whether name is a CopyName in format f as Validate pins it, so a stored one passes the rules
// it was made by.
func validCopyName(name string, f Format) bool {
	return f.Valid() && strings.HasSuffix(name, "."+string(f)) && CopyName(name, f) == name
}
