package lesson

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClassifyTitle(t *testing.T) {
	tests := []struct {
		title   string
		subject string
		kind    Kind
	}{
		// Kind after a separator or in brackets at the end.
		{"Матанализ, лекция", "Матанализ", KindLecture},
		{"Матанализ (лек.)", "Матанализ", KindLecture},
		{"Матанализ [сем]", "Матанализ", KindSeminar},
		{"Физика - практика", "Физика", KindPractice},
		{"Физика — пр.", "Физика", KindPractice},
		{"Химия – лабораторная работа", "Химия", KindLab},
		{"Английский язык, Пр", "Английский язык", KindPractice},
		{"Calculus (lab)", "Calculus", KindLab},
		{"Calculus, Seminar", "Calculus", KindSeminar},
		{"История России (ЛК)", "История России", KindLecture},
		{"Матанализ: семинар", "Матанализ", KindSeminar},

		// Kind first: before a separator, in brackets at the start, or with a topic.
		{"Лекция: Матанализ", "Матанализ", KindLecture},
		{"Lecture: Calculus", "Calculus", KindLecture},
		{"Семинар - История", "История", KindSeminar},
		{"[лаб] Физика", "Физика", KindLab},
		{"(Сем.) Экономика", "Экономика", KindSeminar},
		{"Лекция по физике", "Физике", KindLecture},
		{"Семинар по физике", "Физике", KindSeminar},
		{"Лабораторная работа по химии", "Химии", KindLab},
		{"Лаб. работа по химии", "Химии", KindLab},
		{"Практическое занятие по истории России", "Истории России", KindPractice},
		{"Lab work on optics", "Optics", KindLab},
		{"Lecture on Calculus", "Calculus", KindLecture},

		// The whole title is a kind: the title stays the subject.
		{"Лекция", "Лекция", KindLecture},
		{"Практикум", "Практикум", KindPractice},

		// Not recognized: unknown, the title is the subject.
		{"Лабиринты истории", "Лабиринты истории", KindUnknown},
		{"Практическая философия", "Практическая философия", KindUnknown},
		{"Лекционный зал 5", "Лекционный зал 5", KindUnknown},
		{"Семинария", "Семинария", KindUnknown},
		{"Матанализ лекция", "Матанализ лекция", KindUnknown},
		{"Физика: механика", "Физика: механика", KindUnknown},
		{"История (1 курс)", "История (1 курс)", KindUnknown},
		{"Матанализ, лекция 3", "Матанализ, лекция 3", KindUnknown},
		{"Pre-lab safety", "Pre-lab safety", KindUnknown},
		{"Физика-практика", "Физика-практика", KindUnknown},
		{"Задачи по физике", "Задачи по физике", KindUnknown},
		{"Lab", "Lab", KindLab},
		{"Другое: Физика", "Другое: Физика", KindUnknown},
		{"(лек)", "(лек)", KindUnknown},
		{"Лекция по", "Лекция по", KindUnknown},

		// Layout: spaces are collapsed and the subject is trimmed of separators.
		{"  Матанализ   ,   лекция  ", "Матанализ", KindLecture},
		{"Матанализ, (лек)", "Матанализ", KindLecture},
		{"", "", KindUnknown},
		{"   ", "", KindUnknown},
	}
	for _, tt := range tests {
		subject, kind := ClassifyTitle(tt.title)
		if subject != tt.subject || kind != tt.kind {
			t.Errorf("ClassifyTitle(%q) = %q, %v; want %q, %v", tt.title, subject, kind, tt.subject, tt.kind)
		}
	}
}

func TestClassifyTitleSubjectIsValid(t *testing.T) {
	for _, title := range []string{
		strings.Repeat("Матанализ ", 50) + "(лек.)",
		strings.Repeat("я", MaxTitleBytes/2),
		"Физика\u202e, лекция",
	} {
		subject, _ := ClassifyTitle(title)
		if err := ValidateSubject(subject); err != nil || utf8.RuneCountInString(subject) > MaxSubjectRunes {
			t.Errorf("ClassifyTitle(%q) subject %q: %v", title, subject, err)
		}
	}
}
