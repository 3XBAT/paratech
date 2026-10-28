package values

import (
	"fmt"
	"slices"
)

// CatalogVersion is the tool catalog version this desktop implements (shared/testdata/tool_catalog_v9.json). v9 has
// the tools of v8 and lets a step carry mentions and a history summary turn (panel prompt v9, chats; TASK-244,
// TASK-245); v8 has the tools of v7 and asks the subject of an assignment (TASK-233, TASK-234); v7 is v6 with
// reply.text of the type reply_text, up to 4000 characters (TASK-223); v6 added create_assignment and the argument type
// time (TASK-216, ADR-0029).
const CatalogVersion = 9

// Tool is a tool of the closed catalog. A name outside it is ErrUnknownTool.
type Tool string

// Tools of catalog v7, in catalog order. ToolGenerateDraft left the catalog in v4 (the assignment page keeps «Быстрый
// черновик»): the planner can no longer call it, but stored actions, plans and calls that name it still read
// (RetiredTools).
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
	ToolGenerateDraft          Tool = "generate_draft" // retired in v4
	ToolEditDue                Tool = "edit_due"
	ToolCreateAssignment       Tool = "create_assignment" // since v6
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
func Tools() []Tool { return toolsOf(catalog) }

// RetiredTools returns the tools that left the catalog but may still be named by stored data.
func RetiredTools() []Tool { return toolsOf(retired) }

func toolsOf(specs []ToolSpec) []Tool {
	out := make([]Tool, len(specs))
	for i, s := range specs {
		out[i] = s.tool
	}
	return out
}

// ParseTool returns the tool named s. A retired tool is ErrUnknownTool: the planner cannot call it.
func ParseTool(s string) (Tool, error) {
	t := Tool(s)
	if _, ok := specOf(t); !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownTool, s)
	}
	return t, nil
}

// Valid reports whether t is in the catalog or retired, so stored calls, plans and actions of an older catalog
// restore. Planner input goes through ParseTool, which knows the current catalog only.
func (t Tool) Valid() bool {
	_, ok := storedSpecOf(t)
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
	ArgTime      ArgType = "time" // HH:MM, 24 h; only together with an argument "due" (catalog v6)
	ArgEnum      ArgType = "enum"
	ArgLabel     ArgType = "label"
	ArgQuery     ArgType = "query" // the student's question or search text (since catalog v2)
	ArgText      ArgType = "text"
	ArgReplyText ArgType = "reply_text" // reply.text only: up to MaxReplyTextRunes, angle brackets allowed (catalog v7)
	ArgTextList  ArgType = "text_list"
	ArgCardIDs   ArgType = "card_ids" // 0..MaxCardIDs IDs a card lists; the desktop shows their local data (catalog v3)
	ArgPlanSteps ArgType = "plan_steps"
	ArgQuestions ArgType = "questions"
)

// ArgSpec describes one argument of a tool.
type ArgSpec struct {
	Name     string
	Type     ArgType
	Required bool
	Values   []string // enum values
	Kind     IDKind   // ID kind of an id, ids or card_ids argument
	// Persistent marks a label that is stored and later shown to the planner (subject, folder and semester names);
	// it is parsed as a Name, not a Label.
	Persistent bool
}

// ToolSpec is the argument schema of a tool.
type ToolSpec struct {
	tool  Tool
	args  []ArgSpec
	oneOf [][]string // groups of optional arguments of which exactly one must be given
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

// OneOf returns the groups of arguments of which exactly one must be given.
func (s ToolSpec) OneOf() [][]string {
	out := make([][]string, len(s.oneOf))
	for i, g := range s.oneOf {
		out[i] = slices.Clone(g)
	}
	return out
}

// oneOfGroup returns the one-of group that holds name.
func (s ToolSpec) oneOfGroup(name string) ([]string, bool) {
	for _, g := range s.oneOf {
		if slices.Contains(g, name) {
			return g, true
		}
	}
	return nil, false
}

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

func specOf(t Tool) (ToolSpec, bool) { return findSpec(catalog, t) }

// storedSpecOf finds the schema of a tool in the catalog or among the retired ones, for stored data only.
func storedSpecOf(t Tool) (ToolSpec, bool) {
	if s, ok := findSpec(catalog, t); ok {
		return s, true
	}
	return findSpec(retired, t)
}

func findSpec(specs []ToolSpec, t Tool) (ToolSpec, bool) {
	for _, s := range specs {
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

// catalog is tool catalog v9 (shared/testdata/tool_catalog_v9.json; a test in desktop/tests compares them). It is
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
	{tool: ToolSearch, args: []ArgSpec{typed("query", ArgQuery, true), enum("scope", false, "subject", "all")}},
	{tool: ToolAnswerQuestion, args: []ArgSpec{typed("question", ArgQuery, true), enum("scope", false, "subject", "all")}},
	{tool: ToolInspectUpload, args: []ArgSpec{id("upload", KindUpload, true)}},
	{tool: ToolCreateSubject, args: []ArgSpec{persistent("name", true), persistent("semester", false)}},
	{tool: ToolCreateFolder, args: []ArgSpec{id("subject", KindSubject, true), persistent("name", true)}},
	{tool: ToolPlaceUpload, args: []ArgSpec{
		id("upload", KindUpload, true), id("subject", KindSubject, true), id("folder", KindFolder, false),
		id("lesson", KindLesson, false),
	}, oneOf: [][]string{{"folder", "lesson"}}},
	{tool: ToolLinkMaterial, args: []ArgSpec{id("material", KindMaterial, true), id("lesson", KindLesson, true)}},
	{tool: ToolSetLessonInfo, args: []ArgSpec{
		id("recording", KindRecording, true), id("subject", KindSubject, true), enum("kind", true, lessonKinds...),
	}},
	{tool: ToolMakeSummary, args: []ArgSpec{
		id("recording", KindRecording, false), id("material", KindMaterial, false),
	}, oneOf: [][]string{{"recording", "material"}}},
	{tool: ToolMakeLectureText, args: []ArgSpec{id("recording", KindRecording, true)}},
	{tool: ToolFindHomework, args: []ArgSpec{
		id("recording", KindRecording, false), id("material", KindMaterial, false),
	}, oneOf: [][]string{{"recording", "material"}}},
	{tool: ToolEditDue, args: []ArgSpec{id("assignment", KindAssignment, true), typed("due", ArgDate, true)}},
	// title and note (v6) are the student's own words: display text of the assignment, never a recipient or a path.
	{tool: ToolCreateAssignment, args: []ArgSpec{
		typed("title", ArgLabel, true), id("subject", KindSubject, false), id("lesson", KindLesson, false),
		typed("due", ArgDate, false), typed("time", ArgTime, false), typed("note", ArgQuery, false),
	}},
	// topic (catalog v4) is the student's words for a work without an assignment: free model text, a Query.
	{tool: ToolTriageAssignment, args: []ArgSpec{
		id("assignment", KindAssignment, false), id("upload", KindUpload, false), typed("topic", ArgQuery, false),
	}, oneOf: [][]string{{"assignment", "upload", "topic"}}},
	{tool: ToolWriteWork, args: []ArgSpec{id("work", KindWork, true)}},
	{tool: ToolProposeDeadline, args: []ArgSpec{id("assignment", KindAssignment, true)}},
	{tool: ToolProposeSend, args: []ArgSpec{id("assignment", KindAssignment, true), id("version", KindVersion, true)}},
	{tool: ToolProposeDeleteRecording, args: []ArgSpec{id("recording", KindRecording, true)}},
	{tool: ToolShowPlan, args: []ArgSpec{
		typed("summary", ArgText, true), typed("steps", ArgPlanSteps, true), typed("notes", ArgTextList, false),
	}},
	{tool: ToolAskQuestions, args: []ArgSpec{typed("questions", ArgQuestions, true)}},
	{tool: ToolReply, args: []ArgSpec{
		typed("text", ArgReplyText, true), {Name: "assignments", Type: ArgCardIDs, Kind: KindAssignment},
	}},
	{tool: ToolCannotDo, args: []ArgSpec{typed("your_part", ArgTextList, true), typed("our_part", ArgTextList, true)}},
}

// retired are the schemas of tools that left the catalog, as their last catalog had them: a stored plan step that
// names one still decodes. Read-only like catalog.
var retired = []ToolSpec{
	{tool: ToolGenerateDraft, args: []ArgSpec{id("assignment", KindAssignment, true), typed("instruction", ArgLabel, false)}},
}
