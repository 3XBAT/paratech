package values

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Wave 11 of agent parity (ADR-0033 §8, §11; ADR-0038 §6 without set_call_link / clear_call_link): catalog v11 is v10
// plus the tools below, reply.chat_title and two more settings of set_setting. It is declared, not served:
// CatalogVersion stays CatalogWave10 until the desktop switch task (TASK-443), so Tools, ParseTool, SpecOf and
// ParseCall do not know these tools; ToolsAt(11) and ParseCallAt(…, 11) do. The order of the Hub golden
// (shared/testdata/tool_catalog_v11.json, TASK-442) will win over the order here.
// A reply with an invalid chat_title keeps the reply and drops the title; the Hub schema (TASK-442) must accept and
// ignore it the same way or refuse it, the desktop drop is only defensive.

// CatalogWave11 is the catalog version of wave 11.
const CatalogWave11 = 11

// Tools of wave 11, in the order of ADR-0033 §11.
const (
	ToolSetTeacher      Tool = "set_teacher"       // C: the card shows the exact name and address
	ToolClearTeacher    Tool = "clear_teacher"     // C
	ToolSetProfileField Tool = "set_profile_field" // C: the card shows the exact value
	ToolMoveToInbox     Tool = "move_to_inbox"
	ToolUnlinkMaterial  Tool = "unlink_material"
	ToolRemoveMaterial  Tool = "remove_material" // soft; Undo restores
	ToolAddAlias        Tool = "add_alias"       // a suggestion ID, never the model's text
	ToolRemoveFolder    Tool = "remove_folder"
	ToolRemoveSubject   Tool = "remove_subject" // with notes or files: always a plan (actionpolicy)
)

// Argument types of catalog v11. The values are typed by the model from the student's request (ADR-0033 §8): only
// their format and length are checked here, the page's own rules run in the use case. Errors never carry the value,
// and the value types print redacted.
const (
	ArgEmail        ArgType = "email"         // set_teacher.email
	ArgCallLink     ArgType = "call_link"     // a link to an online class (rows of set_call_links, TASK-434)
	ArgProfileValue ArgType = "profile_value" // set_profile_field.value
	ArgChatTitle    ArgType = "chat_title"    // reply.chat_title; an invalid one is dropped, not an error
)

// Limits of the wave-11 values.
const (
	MaxEmailBytes        = 254  // RFC 5321 path limit, subject.MaxTeacherEmailLen
	MaxCallLinkBytes     = 2048 // ADR-0038 §4
	MaxProfileValueRunes = 200  // settings.MaxProfileRunes, the longest profile field
	MaxChatTitleRunes    = 60   // ADR-0033 §8
	redactedValue        = "[redacted]"
)

// TeacherRole is the kind of lessons a teacher of a subject gives: the codes of subject.TeacherRole (own copy,
// ARCH-11).
type TeacherRole string

// Teacher roles, in the order of the subject page.
const (
	TeacherLecture  TeacherRole = "lecture"
	TeacherPractice TeacherRole = "practice"
	TeacherLab      TeacherRole = "lab"
)

// TeacherRoles returns every role in order.
func TeacherRoles() []TeacherRole { return []TeacherRole{TeacherLecture, TeacherPractice, TeacherLab} }

// ProfileField is a field of «Профиль студента» (settings.ProfileFields; own copy of the codes, ARCH-11).
type ProfileField string

// Profile fields, in the order of the page.
const (
	ProfileFullName   ProfileField = "full_name"
	ProfileGroup      ProfileField = "group"
	ProfileUniversity ProfileField = "university"
	ProfileFaculty    ProfileField = "faculty"
	ProfileCity       ProfileField = "city"
)

// profileFieldRunes is the length limit of every profile field (settings.MaxProfileRunes is one limit for all).
var profileFieldRunes = map[ProfileField]int{
	ProfileFullName: MaxProfileValueRunes, ProfileGroup: MaxProfileValueRunes, ProfileUniversity: MaxProfileValueRunes,
	ProfileFaculty: MaxProfileValueRunes, ProfileCity: MaxProfileValueRunes,
}

// ProfileFields returns every field in order.
func ProfileFields() []ProfileField {
	return []ProfileField{ProfileFullName, ProfileGroup, ProfileUniversity, ProfileFaculty, ProfileCity}
}

// ProfileFieldRunes returns the length limit of field f; 0 for an unknown field.
func ProfileFieldRunes(f ProfileField) int { return profileFieldRunes[f] }

// Settings of wave 11 (set_setting of catalog v11).
const (
	SettingCalendarOnDueChange Setting = "calendar_update_on_due_change" // settings.CalendarOnDueChange (TASK-276)
	SettingNotesOCR            Setting = "notes_ocr"                     // settings.NoteRecognition (TASK-294)
)

// Values of the wave-11 settings. notes_ocr has no cloud value for the agent: images would leave the computer, so a
// future cloud engine is the page's choice only (TASK-331).
const (
	CalendarAsk    = "ask"
	CalendarUpdate = "update"
	CalendarKeep   = "keep"
	NotesOCRLocal  = "local"
)

// SettingsAt returns the settings set_setting of catalog version v may change: the three toggles of wave 10, plus the
// two of wave 11 from catalog v11 on.
func SettingsAt(v int) []Setting {
	out := []Setting{SettingRecordingPrompts, SettingACPowerOnly, SettingRecognitionLoad}
	if v >= CatalogWave11 {
		out = append(out, SettingCalendarOnDueChange, SettingNotesOCR)
	}
	return out
}

// Email is an e-mail address the model typed (set_teacher.email). It prints redacted.
type Email string

// ParseEmail trims s and checks its format only: one line of 3..MaxEmailBytes bytes without control or space
// characters, a single @ with text on both sides. The ADR-0030 rules run in the use case.
func ParseEmail(s string) (Email, error) {
	s = strings.TrimSpace(s)
	if len(s) > MaxEmailBytes || !utf8.ValidString(s) {
		return "", fmt.Errorf("%w: an e-mail address must have at most %d bytes", ErrInvalidText, MaxEmailBytes)
	}
	if strings.ContainsFunc(s, notInToken) {
		return "", fmt.Errorf("%w: an e-mail address has a space, control or format character", ErrInvalidText)
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", fmt.Errorf("%w: an e-mail address has one @ between two parts", ErrInvalidText)
	}
	return Email(s), nil
}

// CallLink is a link to an online class the model typed (ADR-0038 §4). It prints redacted.
type CallLink string

// ParseCallLink trims s and checks its format only: one line of 1..MaxCallLinkBytes bytes without control or space
// characters. subject.ParseCallLink runs in the use case.
func ParseCallLink(s string) (CallLink, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxCallLinkBytes || !utf8.ValidString(s) {
		return "", fmt.Errorf("%w: a call link must have 1..%d bytes", ErrInvalidText, MaxCallLinkBytes)
	}
	if strings.ContainsFunc(s, notInToken) {
		return "", fmt.Errorf("%w: a call link has a space, control or format character", ErrInvalidText)
	}
	return CallLink(s), nil
}

// ProfileValue is the text of a profile field the model typed (set_profile_field.value). It prints redacted.
type ProfileValue string

// ParseProfileValue trims s and checks it as the profile page does: one line of 1..MaxProfileValueRunes characters of
// valid UTF-8 without control, format (bidi, zero-width) or line separator characters. The limit of the field is
// checked with the field (checkProfileField).
func ParseProfileValue(s string) (ProfileValue, error) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxProfileValueRunes {
		return "", fmt.Errorf("%w: a profile value must have 1..%d characters", ErrInvalidText, MaxProfileValueRunes)
	}
	if strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 || r == utf8.RuneError
	}) {
		return "", fmt.Errorf("%w: a profile value must be one line without control characters", ErrInvalidText)
	}
	return ProfileValue(s), nil
}

// ChatTitle is the title the agent gives a new chat (reply.chat_title, ADR-0033 §8): plain text, one line.
type ChatTitle string

// ParseChatTitle collapses every run of white space (line breaks included) to one space, trims s and checks it:
// 1..MaxChatTitleRunes printable characters, no control, format (bidi, zero-width) or separator character, no HTML
// angle bracket.
func ParseChatTitle(s string) (ChatTitle, error) {
	s = strings.Join(strings.Fields(s), " ")
	if err := checkPrintable(s, MaxChatTitleRunes, false, false); err != nil {
		return "", err
	}
	return ChatTitle(s), nil
}

// notInToken reports whether r may not appear in an address or a link: a space, a control, format or separator
// character, or the mark of broken UTF-8.
func notInToken(r rune) bool {
	return unicode.IsSpace(r) || !unicode.IsPrint(r) || r == utf8.RuneError
}

// String hides the address.
func (Email) String() string { return redactedValue }

// Format prints String for every verb.
func (e Email) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, e.String()) }

// LogValue keeps the address out of slog.
func (e Email) LogValue() slog.Value { return slog.StringValue(e.String()) }

// String hides the link.
func (CallLink) String() string { return redactedValue }

// Format prints String for every verb.
func (l CallLink) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, l.String()) }

// LogValue keeps the link out of slog.
func (l CallLink) LogValue() slog.Value { return slog.StringValue(l.String()) }

// String hides the value.
func (ProfileValue) String() string { return redactedValue }

// Format prints String for every verb.
func (p ProfileValue) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, p.String()) }

// LogValue keeps the value out of slog.
func (p ProfileValue) LogValue() slog.Value { return slog.StringValue(p.String()) }

// Email returns an email argument.
func (a Args) Email(name string) (Email, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgEmail {
		return "", false
	}
	return Email(v.str), true
}

// CallLink returns a call_link argument.
func (a Args) CallLink(name string) (CallLink, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgCallLink {
		return "", false
	}
	return CallLink(v.str), true
}

// ProfileValue returns a profile_value argument.
func (a Args) ProfileValue(name string) (ProfileValue, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgProfileValue {
		return "", false
	}
	return ProfileValue(v.str), true
}

// ChatTitle returns a chat_title argument; absent also when the planner gave an invalid one.
func (a Args) ChatTitle(name string) (ChatTitle, bool) {
	v, ok := a.vals[name]
	if !ok || v.typ != ArgChatTitle {
		return "", false
	}
	return ChatTitle(v.str), true
}

// Format prints the argument names only: some values are the student's personal data (ADR-0033 §8).
func (a Args) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "args("+strings.Join(a.Names(), ",")+")")
}

// Format prints the call ID, the tool and the argument names, never a value.
func (c Call) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "call(%s %s %v)", c.id, c.tool, c.args)
}

// parseValue11 parses an argument of a wave-11 type.
func parseValue11(as ArgSpec, raw json.RawMessage) (argValue, error) {
	v := argValue{typ: as.Type}
	s, err := decodeString(raw)
	if err != nil {
		return v, err
	}
	switch as.Type {
	case ArgEmail:
		e, err := ParseEmail(s)
		if err != nil {
			return v, err
		}
		v.str, v.canon = string(e), string(e)
	case ArgCallLink:
		l, err := ParseCallLink(s)
		if err != nil {
			return v, err
		}
		v.str, v.canon = string(l), string(l)
	case ArgProfileValue:
		p, err := ParseProfileValue(s)
		if err != nil {
			return v, err
		}
		v.str, v.canon = string(p), string(p)
	case ArgChatTitle:
		t, err := ParseChatTitle(s)
		if err != nil {
			return v, err
		}
		v.str, v.canon = string(t), string(t)
	default:
		return v, fmt.Errorf("%w: unsupported type", ErrInvalidArguments)
	}
	return v, nil
}

// checkProfileField refuses a value longer than its field allows; the error names the field, never the value.
func checkProfileField(vals map[string]argValue) error {
	f, v := ProfileField(vals["field"].str), vals["value"].str
	limit := ProfileFieldRunes(f)
	if limit == 0 || utf8.RuneCountInString(v) > limit {
		return fmt.Errorf("%w: value is longer than the profile field %s allows", ErrInvalidArguments, f)
	}
	return nil
}

func teacherRoleValues() []string {
	out := make([]string, 0, len(TeacherRoles()))
	for _, r := range TeacherRoles() {
		out = append(out, string(r))
	}
	return out
}

func profileFieldValues() []string {
	out := make([]string, 0, len(ProfileFields()))
	for _, f := range ProfileFields() {
		out = append(out, string(f))
	}
	return out
}

// reply11 is reply of catalog v11: v10 plus the optional title of a new chat.
var reply11 = ToolSpec{tool: ToolReply, args: []ArgSpec{
	typed("text", ArgReplyText, true), {Name: "assignments", Type: ArgCardIDs, Kind: KindAssignment},
	typed("chat_title", ArgChatTitle, false),
}}

// setSetting11 is set_setting of catalog v11: the settings of SettingsAt(11) and the union of their values.
var setSetting11 = ToolSpec{tool: ToolSetSetting, args: []ArgSpec{
	enum("setting", true, settingNames(CatalogWave11)...), enum("value", true, settingValueEnum(CatalogWave11)...),
}}

// wave11 are the schemas of the wave-11 tools. set_teacher takes both the name and the address, as the subject page
// (subject.NewTeacher needs both); the name is the student's words, a label.
var wave11 = []ToolSpec{
	// The name is a label of at most 80 characters on purpose (the page allows 200; a longer name needs its own type)
	// and it is stored model text.
	{tool: ToolSetTeacher, args: []ArgSpec{
		id("subject", KindSubject, true), enum("role", true, teacherRoleValues()...), typed("name", ArgLabel, true),
		typed("email", ArgEmail, true),
	}},
	{tool: ToolClearTeacher, args: []ArgSpec{id("subject", KindSubject, true), enum("role", true, teacherRoleValues()...)}},
	{tool: ToolSetProfileField, args: []ArgSpec{
		enum("field", true, profileFieldValues()...), typed("value", ArgProfileValue, true),
	}},
	{tool: ToolMoveToInbox, args: []ArgSpec{id("material", KindMaterial, true)}},
	{tool: ToolUnlinkMaterial, args: []ArgSpec{id("material", KindMaterial, true), id("lesson", KindLesson, true)}},
	{tool: ToolRemoveMaterial, args: []ArgSpec{id("material", KindMaterial, true)}},
	{tool: ToolAddAlias, args: []ArgSpec{id("subject", KindSubject, true), id("suggestion", KindSuggestion, true)}},
	{tool: ToolRemoveFolder, args: []ArgSpec{id("subject", KindSubject, true), id("folder", KindFolder, true)}},
	{tool: ToolRemoveSubject, args: []ArgSpec{id("subject", KindSubject, true)}},
}

// catalog11 is tool catalog v11: v10 with reply and set_setting of v11, then the wave. Read-only like catalog.
var catalog11 = func() []ToolSpec {
	out := make([]ToolSpec, 0, len(catalog10)+len(wave11))
	for _, s := range catalog10 {
		switch s.tool {
		case ToolReply:
			s = reply11
		case ToolSetSetting:
			s = setSetting11
		}
		out = append(out, s)
	}
	return append(out, wave11...)
}()
