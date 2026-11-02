package placement

import (
	"strings"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// marker is a lesson-kind marker of a file name, one token in the values.Key form. prefix: any token that starts
// with it («лабораторная», «лабораторные»); otherwise the whole token only, so «сем» never matches «Семёнов».
//
// It is a table of its own, not schedulesubjects' marks: those read schedule strings («Физика (лек.)», dots and
// separators matter), these read file names, and «Билеты» and «ДЗ» are no lesson kinds.
type marker struct {
	text   string
	folder subject.System
	prefix bool
}

// markers is the one table of the markers of file names.
var markers = []marker{
	{"лр", subject.SystemLabs, false},
	{"лаб", subject.SystemLabs, false},
	{"лаба", subject.SystemLabs, false},
	{"лабы", subject.SystemLabs, false},
	{"лаборатор", subject.SystemLabs, true},

	{"сем", subject.SystemSeminars, false},
	{"семинар", subject.SystemSeminars, true},
	{"пз", subject.SystemSeminars, false},
	{"практ", subject.SystemSeminars, true},

	{"лек", subject.SystemLectures, false},
	{"лекц", subject.SystemLectures, true},

	{"билет", subject.SystemTickets, true},

	{"дз", subject.SystemHomework, false},
	{"домашн", subject.SystemHomework, true},
	{"домашка", subject.SystemHomework, false},
}

// markerOf returns the folder a token names; ok is false for a token that is no marker.
func markerOf(token string) (subject.System, bool) {
	for _, m := range markers {
		if token == m.text || (m.prefix && strings.HasPrefix(token, m.text)) {
			return m.folder, true
		}
	}
	return "", false
}

// folderKind is the folder the markers of file name say, the tokens of the match itself aside («Практикум» as a
// subject is no seminar). No marker, or markers of two folders, is «Материалы»: never guess.
func folderKind(file []string, match span) subject.System {
	var found subject.System
	for i, t := range file {
		if i >= match.from && i < match.to {
			continue
		}
		f, ok := markerOf(t)
		switch {
		case !ok:
		case found == "":
			found = f
		case found != f:
			return subject.SystemMaterials
		}
	}
	if found == "" {
		return subject.SystemMaterials
	}
	return found
}

// trimMarkers drops marker tokens at both ends of the words of a schedule string («Сети (лаб.)»), unless nothing
// would stay.
func trimMarkers(words []string) []string {
	isMarker := func(t string) bool { _, ok := markerOf(t); return ok }
	from, to := 0, len(words)
	for from < to && isMarker(words[from]) {
		from++
	}
	for to > from && isMarker(words[to-1]) {
		to--
	}
	if from == to {
		return words
	}
	return words[from:to]
}

// systemFolder returns the id of the system folder sys among folders.
func systemFolder(folders []Folder, sys subject.System) (shared.FolderID, bool) {
	for _, f := range folders {
		if f.System == sys && sys != "" {
			return f.ID, true
		}
	}
	return "", false
}
