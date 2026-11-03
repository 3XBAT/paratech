package tests

// Scenario test of «добавь предмет по последней записи» (beta 2026-11-03, TASK-369) on the real SQLite, the real
// library and the real Driver; the Hub is the scripted fake. The planner used to ask the name back, as find_lessons
// rows had none: now the newest recording carries its title, subject name and teacher as data, also when its subject
// is not in «Предметы» yet, and the subject is made from that name with no question.

import (
	"encoding/json"
	"testing"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/testenv"
)

func TestAgentAddsTheSubjectOfTheLastRecording(t *testing.T) {
	e := newActionsEnv(t)
	e.knowledge.lessons = []kb.Lesson{
		{Recording: "r7", StartedAt: libScenarioT0.Add(48 * time.Hour), Title: "Социология культуры (лекция)",
			Subject: "Социология культуры", Kind: "lecture", Ready: kb.Readiness{Transcript: "t7"}},
		{Recording: "r2", StartedAt: libScenarioT0, Subject: "Физика", Kind: "seminar",
			Library: kb.LibrarySubject{ID: e.physics.ID(), Name: "Физика"}},
	}
	e.activity.recordings = []activity.Recording{{ID: "r7", LessonTeacher: "Иванова И. И."}, {ID: "r2"}}
	e.restart(t)

	const name = "Социология культуры"
	e.hub.Then(testenv.AgentCall{ID: "q1", Tool: "find_lessons", Args: `{}`}).
		Then(testenv.AgentCall{ID: "w1", Tool: "create_subject", Args: `{"name":"` + name + `"}`}).
		Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Предмет «` + name + `» добавлен."}`})
	up := e.ask(t, values.Scope{}, "добавь предмет по последней записи, там вроде социология культуры")
	if up.Status != thread.StatusDone {
		t.Fatalf("update = %+v", up)
	}
	for _, c := range up.Cards {
		if c.Kind() == values.CardQuestions {
			t.Fatalf("the student was asked: %+v", up.Cards)
		}
	}

	// What the planner read: the newest row is the recording, without a subject of the library, with its names.
	reqs := e.hub.Requests()
	if len(reqs) != 3 {
		t.Fatalf("hub saw %d requests", len(reqs))
	}
	res := lastTurn(t, reqs[1]).GetResults().GetResults()
	if len(res) != 1 || res[0].GetIsError() {
		t.Fatalf("find_lessons results = %v", res)
	}
	var body struct {
		Lessons []map[string]any `json:"lessons"`
	}
	if err := json.Unmarshal([]byte(res[0].GetResultJson()), &body); err != nil || len(body.Lessons) != 2 {
		t.Fatalf("find_lessons = %s, %v", res[0].GetResultJson(), err)
	}
	newest := body.Lessons[0]
	if newest["recording"] != "r7" || newest["subject"] != nil || newest["subject_name"] != name ||
		newest["title"] != "Социология культуры (лекция)" || newest["teacher"] != "Иванова И. И." {
		t.Fatalf("newest row = %v", newest)
	}
	if older := body.Lessons[1]; older["subject"] != string(e.physics.ID()) || older["subject_name"] != "Физика" {
		t.Fatalf("older row = %v", older)
	}

	// The subject is made by that name.
	if res := lastTurn(t, reqs[2]).GetResults().GetResults(); len(res) != 1 || res[0].GetIsError() {
		t.Fatalf("create_subject results = %v", res)
	}
	list, err := e.lib.Subjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list {
		found = found || s.Name().String() == name
	}
	if len(list) != 2 || !found {
		t.Fatalf("subjects = %d, «%s» made %v", len(list), name, found)
	}
}
