package values

import (
	"fmt"
	"slices"
)

// CatalogVersion is the tool catalog version this desktop implements (shared/testdata/tool_catalog_v1.json).
const CatalogVersion = 1

// Tool is a tool of the closed catalog. A name outside it is ErrUnknownTool.
type Tool string

// Tools of catalog v1, in catalog order.
const (
	ToolListSubjects           Tool = "list_subjects"
	ToolFindLessons            Tool = "find_lessons"
	ToolListAssignments        Tool = "list_assignments"
	ToolJobStatus              Tool = "job_status"
	ToolDeliveryStatus         Tool = "delivery_status"
	ToolSearch                 Tool = "search"
	ToolAnswerQuestion         Tool = "answer_question"
	ToolInspectUpload          Tool = "inspect_upload"
	ToolCreateSubject          Tool = "create_subject"
	ToolCreateFolder           Tool = "create_folder"
	ToolPlaceUpload            Tool = "place_upload"
	ToolLinkMaterial           Tool = "link_material"
	ToolSetLessonInfo          Tool = "set_lesson_info"
	ToolMakeSummary            Tool = "make_summary"
	ToolMakeLectureText        Tool = "make_lecture_text"
	ToolFindHomework           Tool = "find_homework"
	ToolGenerateDraft          Tool = "generate_draft"
	ToolEditDue                Tool = "edit_due"
	ToolTriageAssignment       Tool = "triage_assignment"
	ToolWriteWork              Tool = "write_work"
	ToolProposeDeadline        Tool = "propose_deadline"
	ToolProposeSend            Tool = "propose_send"
	ToolProposeDeleteRecording Tool = "propose_delete_recording"
	ToolShowPlan               Tool = "show_plan"
	ToolAskQuestions           Tool = "ask_questions"
	ToolReply                  Tool = "reply"
	ToolCannotDo               Tool = "cannot_do"
)

// Tools returns every tool in catalog order.
func Tools() []Tool {
	out := make([]Tool, len(catalog))
	for i, s := range catalog {
		out[i] = s.tool
	}
	return out
}

// ParseTool returns the tool named s.
func ParseTool(s string) (Tool, error) {
	t := Tool(s)
	if _, ok := specOf(t); !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownTool, s)
	}
	return t, nil
}

// Valid reports whether t is in the catalog.
func (t Tool) Valid() bool {
	_, ok := specOf(t)
	return ok
}

// String returns the catalog name.
func (t Tool) String() string { return string(t) }

// ArgType is the type of an argument in the catalog.
type ArgType string

// Argument types of the catalog.
const (
	ArgID        ArgType = "id"
	ArgIDs       ArgType = "ids"
	ArgDate      ArgType = "date"
	ArgEnum      ArgType = "enum"
	ArgLabel     ArgType = "label"
	ArgText      ArgType = "text"
	ArgTextList  ArgType = "text_list"
	ArgPlanSteps ArgType = "plan_steps"
	ArgQuestions ArgType = "questions"
)

// ArgSpec describes one argument of a tool.
type ArgSpec struct {
	Name     string
	Type     ArgType
	Required bool
	Values   []string // enum values
	Kind     IDKind   // ID kind of an id or ids argument
	// Persistent marks a label that is stored and later shown to the planner (subject, folder and semester names);
	// it is parsed as a Name, not a Label.
	Persistent bool
}

// ToolSpec is the argument schema of a tool.
type ToolSpec struct {
	tool  Tool
	args  []ArgSpec
	oneOf [][2]string // pairs of optional arguments of which exactly one must be given
}

// Tool returns the tool.
func (s ToolSpec) Tool() Tool { return s.tool }

// Args returns a copy of the argument specs in catalog order.
func (s ToolSpec) Args() []ArgSpec {
	out := make([]ArgSpec, len(s.args))
	for i, a := range s.args {
		a.Values = slices.Clone(a.Values)
		out[i] = a
	}
	return out
}

// OneOf returns the pairs of arguments of which exactly one must be given.
func (s ToolSpec) OneOf() [][2]string { return slices.Clone(s.oneOf) }

func (s ToolSpec) arg(name string) (ArgSpec, bool) {
	for _, a := range s.args {
		if a.Name == name {
			return a, true
		}
	}
	return ArgSpec{}, false
}

// SpecOf returns the schema of a tool.
func SpecOf(t Tool) (ToolSpec, error) {
	s, ok := specOf(t)
	if !ok {
		return ToolSpec{}, fmt.Errorf("%w: %q", ErrUnknownTool, string(t))
	}
	return s, nil
}

func specOf(t Tool) (ToolSpec, bool) {
	for _, s := range catalog {
		if s.tool == t {
			return s, true
		}
	}
	return ToolSpec{}, false
}

var lessonKinds = []string{"lecture", "seminar", "practice", "lab"}

func id(name string, kind IDKind, required bool) ArgSpec {
	return ArgSpec{Name: name, Type: ArgID, Required: required, Kind: kind}
}

func enum(name string, required bool, values ...string) ArgSpec {
	return ArgSpec{Name: name, Type: ArgEnum, Required: required, Values: values}
}

func typed(name string, typ ArgType, required bool) ArgSpec {
	return ArgSpec{Name: name, Type: typ, Required: required}
}

func persistent(name string, required bool) ArgSpec {
	return ArgSpec{Name: name, Type: ArgLabel, Required: required, Persistent: true}
}

// catalog is tool catalog v1 (shared/testdata/tool_catalog_v1.json; a test in desktop/tests compares them). It is
// read-only: accessors return copies.
var catalog = []ToolSpec{
	{tool: ToolListSubjects},
	{tool: ToolFindLessons, args: []ArgSpec{
		id("subject", KindSubject, false), typed("from", ArgDate, false), typed("to", ArgDate, false),
		enum("kind", false, lessonKinds...),
	}},
	{tool: ToolListAssignments, args: []ArgSpec{
		id("subject", KindSubject, false), typed("due_before", ArgDate, false),
		enum("status", false, "upcoming", "overdue", "any"),
	}},
	{tool: ToolJobStatus, args: []ArgSpec{{Name: "ids", Type: ArgIDs, Required: true, Kind: KindJob}}},
	{tool: ToolDeliveryStatus, args: []ArgSpec{id("assignment", KindAssignment, true)}},
	{tool: ToolSearch, args: []ArgSpec{typed("query", ArgLabel, true), enum("scope", false, "subject", "all")}},
	{tool: ToolAnswerQuestion, args: []ArgSpec{typed("question", ArgLabel, true), enum("scope", false, "subject", "all")}},
	{tool: ToolInspectUpload, args: []ArgSpec{id("upload", KindUpload, true)}},
	{tool: ToolCreateSubject, args: []ArgSpec{persistent("name", true), persistent("semester", false)}},
	{tool: ToolCreateFolder, args: []ArgSpec{id("subject", KindSubject, true), persistent("name", true)}},
	{tool: ToolPlaceUpload, args: []ArgSpec{
		id("upload", KindUpload, true), id("subject", KindSubject, true), id("folder", KindFolder, false),
		id("lesson", KindLesson, false),
	}, oneOf: [][2]string{{"folder", "lesson"}}},
	{tool: ToolLinkMaterial, args: []ArgSpec{id("material", KindMaterial, true), id("lesson", KindLesson, true)}},
	{tool: ToolSetLessonInfo, args: []ArgSpec{
		id("recording", KindRecording, true), id("subject", KindSubject, true), enum("kind", true, lessonKinds...),
	}},
	{tool: ToolMakeSummary, args: []ArgSpec{
		id("recording", KindRecording, false), id("material", KindMaterial, false),
	}, oneOf: [][2]string{{"recording", "material"}}},
	{tool: ToolMakeLectureText, args: []ArgSpec{id("recording", KindRecording, true)}},
	{tool: ToolFindHomework, args: []ArgSpec{
		id("recording", KindRecording, false), id("material", KindMaterial, false),
	}, oneOf: [][2]string{{"recording", "material"}}},
	{tool: ToolGenerateDraft, args: []ArgSpec{id("assignment", KindAssignment, true), typed("instruction", ArgLabel, false)}},
	{tool: ToolEditDue, args: []ArgSpec{id("assignment", KindAssignment, true), typed("due", ArgDate, true)}},
	{tool: ToolTriageAssignment, args: []ArgSpec{
		id("assignment", KindAssignment, false), id("upload", KindUpload, false),
	}, oneOf: [][2]string{{"assignment", "upload"}}},
	{tool: ToolWriteWork, args: []ArgSpec{id("work", KindWork, true)}},
	{tool: ToolProposeDeadline, args: []ArgSpec{id("assignment", KindAssignment, true)}},
	{tool: ToolProposeSend, args: []ArgSpec{id("assignment", KindAssignment, true), id("version", KindVersion, true)}},
	{tool: ToolProposeDeleteRecording, args: []ArgSpec{id("recording", KindRecording, true)}},
	{tool: ToolShowPlan, args: []ArgSpec{
		typed("summary", ArgText, true), typed("steps", ArgPlanSteps, true), typed("notes", ArgTextList, false),
	}},
	{tool: ToolAskQuestions, args: []ArgSpec{typed("questions", ArgQuestions, true)}},
	{tool: ToolReply, args: []ArgSpec{typed("text", ArgText, true)}},
	{tool: ToolCannotDo, args: []ArgSpec{typed("your_part", ArgTextList, true), typed("our_part", ArgTextList, true)}},
}
