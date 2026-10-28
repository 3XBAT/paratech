package subject

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var (
	t0       = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)
	autumn26 = values.SemesterOf(t0)
)

func newSubject(t *testing.T, aliases ...string) *Subject {
	t.Helper()
	s, err := New(Spec{ID: "s1", Name: " Физика ", Aliases: aliases, Semester: autumn26}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func folderName(t *testing.T, s string) FolderName {
	t.Helper()
	n, err := NewFolderName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNewHasTheSystemFolders(t *testing.T) {
	s := newSubject(t, "физика", "Физ-ра", "ФИЗ-РА")
	if s.Name().String() != "Физика" || s.Version() != 0 || s.Semester() != autumn26 {
		t.Fatalf("subject %+v", s.State())
	}
	// The alias with the key of the name and the repeated alias are dropped.
	if got := s.State().Aliases; !reflect.DeepEqual(got, []string{"Физ-ра"}) {
		t.Fatalf("aliases = %q", got)
	}
	var names []string
	for _, f := range s.Folders() {
		if !f.IsSystem() || f.ID() != SystemFolderID("s1", f.System()) {
			t.Fatalf("folder %+v", f)
		}
		names = append(names, f.Name().String())
	}
	if want := []string{"Лекции", "Семинары", "Лабораторные", "Материалы", "ДЗ", "Билеты"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("folders = %q, want %q", names, want)
	}
	if f, ok := s.SystemFolder(SystemMaterials); !ok || f.Name().String() != "Материалы" {
		t.Fatalf("SystemFolder(materials) = %+v, %v", f, ok)
	}
	if _, ok := s.SystemFolder(""); ok {
		t.Fatal("SystemFolder(\"\") found an own folder")
	}
}

func TestNewRejects(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want error
	}{
		{"empty name", Spec{ID: "s1", Name: "  ", Semester: autumn26}, ErrInvalidName},
		{"long name", Spec{ID: "s1", Name: strings.Repeat("я", MaxNameRunes+1), Semester: autumn26}, ErrInvalidName},
		{"bad id", Spec{ID: "a b", Name: "Физика", Semester: autumn26}, ErrInvalidSubject},
		{"no semester", Spec{ID: "s1", Name: "Физика"}, ErrInvalidSubject},
		{"empty alias", Spec{ID: "s1", Name: "Физика", Aliases: []string{""}, Semester: autumn26}, ErrInvalidName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.spec, t0); !errors.Is(err, tt.want) {
				t.Fatalf("New = %v, want %v", err, tt.want)
			}
		})
	}
	many := make([]string, MaxAliases+1)
	for i := range many {
		many[i] = "alias " + strings.Repeat("x", i+1)
	}
	if _, err := New(Spec{ID: "s1", Name: "Физика", Aliases: many, Semester: autumn26}, t0); !errors.Is(err, ErrTooMany) {
		t.Fatalf("New with %d aliases = %v, want ErrTooMany", len(many), err)
	}
}

func TestFolderName(t *testing.T) {
	tests := []struct {
		in    string
		want  string
		valid bool
	}{
		{"Конспекты", "Конспекты", true},
		{"  Мои   файлы  ", "Мои файлы", true}, // trailing and repeated spaces are trimmed, not rejected
		{"v1.2 черновик", "v1.2 черновик", true},
		{"console", "console", true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("я", MaxFolderNameRunes), strings.Repeat("я", MaxFolderNameRunes), true},
		{strings.Repeat("я", MaxFolderNameRunes+1), "", false},
		{"a/b", "", false},
		{`a\b`, "", false},
		{"a:b", "", false},
		{"a*b", "", false},
		{"a?b", "", false},
		{`a"b`, "", false},
		{"a<b", "", false},
		{"a>b", "", false},
		{"a|b", "", false},
		{"a\tb", "", false},
		{"a\x00b", "", false},
		{"Папка.", "", false},
		{".", "", false},
		{"..", "", false},
		{"CON", "", false},
		{"nul.txt", "", false},
		{"Com1", "", false},
		{"lpt9 .doc", "", false},
		{"aux", "", false},
	}
	for _, tt := range tests {
		n, err := NewFolderName(tt.in)
		if tt.valid != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidFolderName)) {
			t.Errorf("NewFolderName(%q) = %v, valid %v", tt.in, err, tt.valid)
			continue
		}
		if tt.valid && n.String() != tt.want {
			t.Errorf("NewFolderName(%q) = %q, want %q", tt.in, n.String(), tt.want)
		}
	}
}

func TestAddAndRemoveFolder(t *testing.T) {
	s := newSubject(t)
	t1 := t0.Add(time.Minute)
	f, err := s.AddFolder("f1", folderName(t, "Конспекты"), t1)
	if err != nil || f.IsSystem() || f.ID() != "f1" || s.UpdatedAt() != t1 {
		t.Fatalf("AddFolder = %+v, %v", f, err)
	}
	if got, ok := s.Folder("f1"); !ok || got != f {
		t.Fatalf("Folder(f1) = %+v, %v", got, ok)
	}

	tests := []struct {
		name string
		id   shared.FolderID
		fn   string
		want error
	}{
		{"same name other case", "f2", "КОНСПЕКТЫ", ErrFolderExists},
		{"system name in lower case", "f2", "лекции", ErrFolderExists},
		{"repeated id", "f1", "Другое", ErrInvalidSubject},
		{"bad id", "a/b", "Другое", ErrInvalidSubject},
	}
	for _, tt := range tests {
		if _, err := s.AddFolder(tt.id, folderName(t, tt.fn), t1); !errors.Is(err, tt.want) {
			t.Errorf("%s: AddFolder = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := s.AddFolder("f3", FolderName{}, t1); !errors.Is(err, ErrInvalidFolderName) {
		t.Errorf("AddFolder of the zero name = %v", err)
	}

	lectures := SystemFolderID("s1", SystemLectures)
	if err := s.RemoveFolder(lectures, 0, t1); !errors.Is(err, ErrSystemFolder) {
		t.Fatalf("RemoveFolder(system) = %v", err)
	}
	if err := s.RemoveFolder("nope", 0, t1); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("RemoveFolder(unknown) = %v", err)
	}
	if err := s.RemoveFolder("f1", 1, t1); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveFolder(non-empty) = %v", err)
	}
	if err := s.RemoveFolder("f1", 0, t1); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Folder("f1"); ok || len(s.Folders()) != len(Systems()) {
		t.Fatalf("folders after removal: %+v", s.Folders())
	}
}

func TestOwnFolderLimit(t *testing.T) {
	s := newSubject(t)
	for i := range MaxOwnFolders {
		id := shared.FolderID("f" + strings.Repeat("x", i+1))
		if _, err := s.AddFolder(id, folderName(t, "Папка "+strings.Repeat("x", i+1)), t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddFolder("last", folderName(t, "Ещё одна"), t0); !errors.Is(err, ErrTooMany) {
		t.Fatalf("AddFolder over the limit = %v", err)
	}
}

func TestRemovable(t *testing.T) {
	s := newSubject(t)
	if err := s.Removable(0); err != nil {
		t.Fatal(err)
	}
	if err := s.Removable(2); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("Removable(2) = %v", err)
	}
}

func TestAddAlias(t *testing.T) {
	s := newSubject(t)
	if changed, err := s.AddAlias("Общая физика", t0); err != nil || !changed {
		t.Fatalf("AddAlias = %v, %v", changed, err)
	}
	for _, same := range []string{"ОБЩАЯ  ФИЗИКА", "физика"} {
		if changed, err := s.AddAlias(same, t0); err != nil || changed {
			t.Fatalf("AddAlias(%q) = %v, %v; want no change", same, changed, err)
		}
	}
	if _, err := s.AddAlias(" ", t0); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("AddAlias(blank) = %v", err)
	}
	if !s.HasKey(values.Key("общая физика")) || s.HasKey("химия") {
		t.Fatal("HasKey")
	}
}

func TestRemoveAlias(t *testing.T) {
	t1 := t0.Add(time.Minute)
	tests := []struct {
		name    string
		alias   string
		changed bool
		left    []string
	}{
		{"by key", " общая  ФИЗИКА ", true, []string{"Физика (лаб.)"}},
		{"case", "ФИЗИКА (ЛАБ.)", true, []string{"Общая физика"}},
		{"the name stays", "физика", false, []string{"Общая физика", "Физика (лаб.)"}},
		{"no such alias", "Химия", false, []string{"Общая физика", "Физика (лаб.)"}},
		{"blank", "  ", false, []string{"Общая физика", "Физика (лаб.)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSubject(t, "Общая физика", "Физика (лаб.)")
			if got := s.RemoveAlias(tt.alias, t1); got != tt.changed {
				t.Fatalf("RemoveAlias = %v, want %v", got, tt.changed)
			}
			if got := s.State().Aliases; !reflect.DeepEqual(got, tt.left) {
				t.Fatalf("aliases = %q, want %q", got, tt.left)
			}
			if s.Name().String() != "Физика" {
				t.Fatalf("name = %q", s.Name())
			}
			wantUpdated := t0
			if tt.changed {
				wantUpdated = t1
			}
			if !s.UpdatedAt().Equal(wantUpdated) {
				t.Fatalf("updatedAt = %v, want %v", s.UpdatedAt(), wantUpdated)
			}
		})
	}
}

func TestRestoreRoundTripAndRejects(t *testing.T) {
	s := newSubject(t, "Физ")
	if _, err := s.AddFolder("f1", folderName(t, "Конспекты"), t0); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	st.Version = 3
	back, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.State(), st) || back.Version() != 3 {
		t.Fatalf("Restore = %+v, want %+v", back.State(), st)
	}

	tests := []struct {
		name   string
		modify func(*State)
	}{
		{"version 0", func(st *State) { st.Version = 0 }},
		{"name not clean", func(st *State) { st.Name = " Физика" }},
		{"alias repeats the name", func(st *State) { st.Aliases = []string{"ФИЗИКА"} }},
		{"system folder missing", func(st *State) { st.Folders = st.Folders[1:] }},
		{"system folder renamed", func(st *State) { st.Folders[0].Name = "Лекции 2" }},
		{"system folder id", func(st *State) { st.Folders[0].ID = "x" }},
		{"system folder twice", func(st *State) { st.Folders[6].System = SystemLectures }},
		{"folder name key twice", func(st *State) { st.Folders[6].Name = "лекции" }},
		{"bad folder name", func(st *State) { st.Folders[6].Name = "CON" }},
		{"no semester", func(st *State) { st.Semester = values.Semester{} }},
		{"no time", func(st *State) { st.CreatedAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := s.State()
			st.Version = 1
			st.Aliases = append([]string(nil), st.Aliases...)
			st.Folders = append([]FolderState(nil), st.Folders...)
			tt.modify(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidSubject) {
				t.Fatalf("Restore = %v, want ErrInvalidSubject", err)
			}
		})
	}
}
