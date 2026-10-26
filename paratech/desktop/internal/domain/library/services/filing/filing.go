// Package filing decides where the app files a file it made for the student (a turnkey work in «ДЗ», the answers to a
// ticket set in «Билеты») and whether such a file is still there.
package filing

import (
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Target is the system folder sys of subject s where a file made for subject sub goes. ok is false, and the file
// stays in «Входящие», when there is no subject, the subject is gone (s nil or another subject) or it has no such
// folder.
func Target(sub shared.SubjectID, s *subject.Subject, sys subject.System) (folder shared.FolderID, ok bool) {
	if sub == "" || s == nil || s.ID() != sub {
		return "", false
	}
	f, ok := s.SystemFolder(sys)
	if !ok {
		return "", false
	}
	return f.ID(), true
}

// Filed reports whether m is still where Target puts a file made for sub: in that folder of sub, or in «Входящие»
// when Target finds none. A file moved to another folder or subject, or out of «Входящие», is not.
func Filed(m *material.Material, sub shared.SubjectID, s *subject.Subject, sys subject.System) bool {
	folder, ok := Target(sub, s, sys)
	if !ok {
		return m.InInbox()
	}
	return m.Subject() == sub && m.Folder() == folder
}
