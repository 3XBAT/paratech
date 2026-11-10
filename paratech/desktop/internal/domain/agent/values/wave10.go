package values

import (
	"fmt"
	"slices"

	"paratech/desktop/internal/domain/shared"
)

// Wave 10 of agent parity (ADR-0033 §10): catalog v10 is v9 plus the tools below, list_assignments with a state and
// show_plan of up to MaxPlanSteps steps (already accepted for every catalog since TASK-317). CatalogVersion is v10
// since TASK-326, so Tools, ParseTool, SpecOf and ParseCall serve these tools; ToolsAt(9) still is the v9 set. The
// tool order of the Hub golden (shared/testdata/tool_catalog_v10.json, TASK-325) wins over the order here.

// CatalogWave10 is the catalog version of wave 10.
const CatalogWave10 = 10

// Tools of wave 10, in the order of the ADR-0033 §10 table.
const (
	ToolProposeSubjectsFromSchedule Tool = "propose_subjects_from_schedule"
	ToolChangeAssignment            Tool = "change_assignment"
	ToolMarkAssignmentsDone         Tool = "mark_assignments_done"
	ToolListNotes                   Tool = "list_notes"
	ToolNote                        Tool = "note"
	ToolGetSettings                 Tool = "get_settings"
	ToolSetSetting                  Tool = "set_setting"
	ToolQuotaStatus                 Tool = "quota_status"
	ToolRecording                   Tool = "recording"
	ToolOfferButton                 Tool = "offer_button"
)

// MaxMarkAssignments bounds mark_assignments_done.assignments (ADR-0033 §9; homeworkflow.MaxMarkDone).
const MaxMarkAssignments = 50

// ArgAction is the name of the action enum of change_assignment, note, recording and offer_button.
const ArgAction = "action"

// Actions of change_assignment.
const (
	AssignmentEdit    = "edit"
	AssignmentDelete  = "delete"  // soft (ADR-0029); with a Google event only after «Да» (О-6 б)
	AssignmentRestore = "restore" // only what this chat deleted (ADR-0029 C-2): checked by the dispatch
)

// Actions of note.
const (
	NoteAddText   = "add_text" // the student's words as a text part of «Мой конспект» (§8, О-5 в)
	NoteAddUpload = "add_upload"
	NoteMove      = "move"
	NoteRename    = "rename"
	NoteRemove    = "remove" // soft; Undo restores
)

// Actions of recording.
const (
	RecordingStart = "start"
	RecordingStop  = "stop" // no Undo (ADR-0033 §2, О-7 а)
)

// States of list_assignments (catalog v10); open is the default.
const (
	StateOpen = "open"
	StateDone = "done"
	StateAll  = "all"
)

// Setting is a setting set_setting may change (wave 10: the three toggles of «Настройки»; wave 11: two more,
// SettingsAt).
type Setting string

// Settings of wave 10.
const (
	SettingRecordingPrompts Setting = "recording_prompts" // the popup before a lesson (AC-02)
	SettingACPowerOnly      Setting = "ac_power_only"     // heavy jobs wait for AC power (AC-21)
	SettingRecognitionLoad  Setting = "recognition_load"  // shared.RecognitionLoad
)

// Values of an on/off setting.
const (
	SettingOn  = "on"
	SettingOff = "off"
)

// Settings returns every declared setting (SettingsAt of the newest catalog), so stored actions of any catalog
// restore; set_setting of a catalog takes only SettingsAt(v).
func Settings() []Setting { return SettingsAt(CatalogWave11) }

// Valid reports whether s is one of the settings.
func (s Setting) Valid() bool { return slices.Contains(Settings(), s) }

// SettingValues returns the values setting s takes: on/off for a toggle, the load modes for recognition_load, the
// modes of the wave-11 settings; nil for an unknown setting.
func SettingValues(s Setting) []string {
	switch s {
	case SettingCalendarOnDueChange:
		return []string{CalendarAsk, CalendarUpdate, CalendarKeep}
	case SettingNotesOCR:
		return []string{SettingOff, NotesOCRLocal}
	case SettingRecordingPrompts, SettingACPowerOnly:
		return []string{SettingOn, SettingOff}
	case SettingRecognitionLoad:
		loads := shared.RecognitionLoads()
		out := make([]string, len(loads))
		for i, l := range loads {
			out[i] = string(l)
		}
		return out
	}
	return nil
}

// ValidSettingValue reports whether v is a value of setting s.
func ValidSettingValue(s Setting, v string) bool { return slices.Contains(SettingValues(s), v) }

// settingValueEnum is the value enum of set_setting of catalog v: every value of its settings, each once; the pair is
// checked by checkSetting.
func settingValueEnum(v int) []string {
	var out []string
	for _, s := range SettingsAt(v) {
		for _, v := range SettingValues(s) {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// OfferAction is a button offer_button may show (ADR-0033 §3, closed).
type OfferAction string

// Buttons.
const (
	OfferOpenMaterial    OfferAction = "open_material"
	OfferOpenCallLink    OfferAction = "open_call_link"
	OfferCopyCallLink    OfferAction = "copy_call_link"
	OfferOpenSummary     OfferAction = "open_summary"
	OfferOpenLectureText OfferAction = "open_lecture_text"
	OfferOpenTranscript  OfferAction = "open_transcript"
	OfferSaveAs          OfferAction = "save_as"
	OfferGoogleSignIn    OfferAction = "google_sign_in"
	OfferGoogleSync      OfferAction = "google_sync"
	OfferDownloadModel   OfferAction = "download_model"
	OfferSoundCheck      OfferAction = "sound_check"
	OfferOpenHelp        OfferAction = "open_help"
	OfferUndoAction      OfferAction = "undo_action"
	OfferStartRecording  OfferAction = "start_recording"
)

// offerTarget is the ID argument a button needs; a zero one needs none.
type offerTarget struct {
	arg  string
	kind IDKind
}

// offerTargets maps every button to its ID, in the order of ADR-0033 §3. The call link is resolved by the lesson in
// Go (the planner never sees it); save_as is «Сохранить как» of a turnkey work; undo_action is the «Отменить» of the
// latest undoable action of the chat, which the desktop picks (no action ID reaches the planner).
var offerTargets = []struct {
	action OfferAction
	target offerTarget
}{
	{OfferOpenMaterial, offerTarget{"material", KindMaterial}},
	{OfferOpenCallLink, offerTarget{"lesson", KindLesson}},
	{OfferCopyCallLink, offerTarget{"lesson", KindLesson}},
	{OfferOpenSummary, offerTarget{"recording", KindRecording}},
	{OfferOpenLectureText, offerTarget{"recording", KindRecording}},
	{OfferOpenTranscript, offerTarget{"recording", KindRecording}},
	{OfferSaveAs, offerTarget{"work", KindWork}},
	{OfferGoogleSignIn, offerTarget{}},
	{OfferGoogleSync, offerTarget{}},
	{OfferDownloadModel, offerTarget{}},
	{OfferSoundCheck, offerTarget{}},
	{OfferOpenHelp, offerTarget{}},
	{OfferUndoAction, offerTarget{}},
	{OfferStartRecording, offerTarget{}},
}

// OfferActions returns every button in catalog order.
func OfferActions() []OfferAction {
	out := make([]OfferAction, len(offerTargets))
	for i, o := range offerTargets {
		out[i] = o.action
	}
	return out
}

// OfferTarget returns the ID argument and kind a button needs ("" when none) and whether the button is known.
func OfferTarget(a OfferAction) (arg string, kind IDKind, ok bool) {
	for _, o := range offerTargets {
		if o.action == a {
			return o.target.arg, o.target.kind, true
		}
	}
	return "", "", false
}

// offerIDArgs are the ID arguments of offer_button, each the ID of some buttons.
var offerIDArgs = []ArgSpec{
	id("material", KindMaterial, false), id("lesson", KindLesson, false), id("recording", KindRecording, false),
	id("work", KindWork, false),
}

func offerActionValues() []string {
	out := make([]string, len(offerTargets))
	for i, o := range offerTargets {
		out[i] = string(o.action)
	}
	return out
}

func settingNames(v int) []string {
	out := make([]string, 0, len(SettingsAt(v)))
	for _, s := range SettingsAt(v) {
		out = append(out, string(s))
	}
	return out
}

// listAssignments10 is list_assignments of catalog v10: v9 plus state (rows carry done).
var listAssignments10 = ToolSpec{tool: ToolListAssignments, args: []ArgSpec{
	id("subject", KindSubject, false), typed("due_before", ArgDate, false),
	enum("status", false, "upcoming", "overdue", "any"), enum("state", false, StateOpen, StateDone, StateAll),
}}

// wave10 are the schemas of the wave-10 tools. title and note of change_assignment are the student's words as in
// create_assignment; note.text is a reply_text (≤ MaxReplyTextRunes, §8).
var wave10 = []ToolSpec{
	{tool: ToolProposeSubjectsFromSchedule},
	{tool: ToolChangeAssignment, args: []ArgSpec{
		id("assignment", KindAssignment, true),
		enum(ArgAction, true, AssignmentEdit, AssignmentDelete, AssignmentRestore),
		typed("title", ArgLabel, false), typed("note", ArgQuery, false), typed("due", ArgDate, false),
		typed("time", ArgTime, false), id("subject", KindSubject, false),
	}},
	{tool: ToolMarkAssignmentsDone, args: []ArgSpec{
		{Name: "assignments", Type: ArgIDs, Required: true, Kind: KindAssignment, MaxItems: MaxMarkAssignments},
		typed("done", ArgBool, true),
	}},
	{tool: ToolListNotes, args: []ArgSpec{
		id("lesson", KindLesson, false), id("subject", KindSubject, false),
	}, oneOf: [][]string{{"lesson", "subject"}}},
	{tool: ToolNote, args: []ArgSpec{
		enum(ArgAction, true, NoteAddText, NoteAddUpload, NoteMove, NoteRename, NoteRemove),
		id("note", KindNote, false), id("lesson", KindLesson, false), typed("date", ArgDate, false),
		id("upload", KindUpload, false), typed("text", ArgReplyText, false), typed("title", ArgLabel, false),
	}},
	{tool: ToolGetSettings},
	{tool: ToolSetSetting, args: []ArgSpec{
		enum("setting", true, settingNames(CatalogWave10)...), enum("value", true, settingValueEnum(CatalogWave10)...),
	}},
	{tool: ToolQuotaStatus},
	{tool: ToolRecording, args: []ArgSpec{enum(ArgAction, true, RecordingStart, RecordingStop)}},
	{tool: ToolOfferButton, args: append([]ArgSpec{enum(ArgAction, true, offerActionValues()...)}, offerIDArgs...)},
}

// catalog10 is tool catalog v10: v9 with list_assignments of v10, then the wave. Read-only like catalog.
var catalog10 = func() []ToolSpec {
	out := make([]ToolSpec, 0, len(catalog)+len(wave10))
	for _, s := range catalog {
		if s.tool == ToolListAssignments {
			s = listAssignments10
		}
		out = append(out, s)
	}
	return append(out, wave10...)
}()

// actionRule lists the arguments one action of a tool takes besides the action: need all, may any, exactly one of
// each group in one, at least one of some. Any other argument is refused.
type actionRule struct {
	need []string
	may  []string
	one  [][]string
	some []string
}

func (r actionRule) allowed(name string) bool {
	if name == ArgAction || slices.Contains(r.need, name) || slices.Contains(r.may, name) || slices.Contains(r.some, name) {
		return true
	}
	return slices.ContainsFunc(r.one, func(g []string) bool { return slices.Contains(g, name) })
}

// changeAssignmentRules: an edit changes at least one field; delete and restore take the assignment only.
var changeAssignmentRules = map[string]actionRule{
	AssignmentEdit:    {need: []string{"assignment"}, some: []string{"title", "note", "due", "time", "subject"}},
	AssignmentDelete:  {need: []string{"assignment"}},
	AssignmentRestore: {need: []string{"assignment"}},
}

// noteTargets are where an added part goes: an existing note, a lesson or a day of the subject in scope.
var noteTargets = []string{"note", "lesson", "date"}

// noteRules: an add goes to exactly one target and may name a new note; move goes to a lesson or a day.
var noteRules = map[string]actionRule{
	NoteAddText:   {need: []string{"text"}, may: []string{"title"}, one: [][]string{noteTargets}},
	NoteAddUpload: {need: []string{"upload"}, may: []string{"title"}, one: [][]string{noteTargets}},
	NoteMove:      {need: []string{"note"}, one: [][]string{{"lesson", "date"}}},
	NoteRename:    {need: []string{"note", "title"}},
	NoteRemove:    {need: []string{"note"}},
}

// offerRules: a button takes exactly the ID it needs and no other.
var offerRules = func() map[string]actionRule {
	out := make(map[string]actionRule, len(offerTargets))
	for _, o := range offerTargets {
		r := actionRule{}
		if o.target.arg != "" {
			r.need = []string{o.target.arg}
		}
		out[string(o.action)] = r
	}
	return out
}()

// checkToolArgs applies the rules that tie arguments of a tool together beyond its schema. open are absent
// arguments that count as given: in a plan step the deferred IDs and the free text the call adds.
func checkToolArgs(t Tool, vals map[string]argValue, open []string) error {
	switch t {
	case ToolChangeAssignment:
		return checkAction(t, vals, open, changeAssignmentRules)
	case ToolNote:
		return checkAction(t, vals, open, noteRules)
	case ToolOfferButton:
		return checkAction(t, vals, open, offerRules)
	case ToolSetSetting:
		return checkSetting(vals)
	case ToolSetProfileField:
		return checkProfileField(vals)
	}
	return nil
}

func checkAction(t Tool, vals map[string]argValue, open []string, rules map[string]actionRule) error {
	act, ok := vals[ArgAction]
	if !ok {
		return fmt.Errorf("%w: %s needs %s", ErrInvalidArguments, t, ArgAction)
	}
	r, ok := rules[act.str]
	if !ok {
		return fmt.Errorf("%w: %s has no rule for its action", ErrInvalidArguments, t)
	}
	has := func(name string) bool {
		_, given := vals[name]
		return given || slices.Contains(open, name)
	}
	for name := range vals {
		if !r.allowed(name) {
			return fmt.Errorf("%w: %s %s does not take %s", ErrInvalidArguments, t, act.str, name)
		}
	}
	for _, name := range r.need {
		if !has(name) {
			return fmt.Errorf("%w: %s %s needs %s", ErrInvalidArguments, t, act.str, name)
		}
	}
	for _, group := range r.one {
		n := 0
		for _, name := range group {
			if has(name) {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("%w: %s %s needs exactly one of %v", ErrInvalidArguments, t, act.str, group)
		}
	}
	if len(r.some) > 0 && !slices.ContainsFunc(r.some, has) {
		return fmt.Errorf("%w: %s %s needs one of %v", ErrInvalidArguments, t, act.str, r.some)
	}
	return nil
}

// checkSetting refuses a value of another setting («recognition_load: on»).
func checkSetting(vals map[string]argValue) error {
	s, v := vals["setting"], vals["value"]
	if !ValidSettingValue(Setting(s.str), v.str) {
		return fmt.Errorf("%w: value is not a value of setting %s", ErrInvalidArguments, s.str)
	}
	return nil
}
