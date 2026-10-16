package urlscheme

import (
	"errors"
	"maps"
	"testing"
)

func TestCommand(t *testing.T) {
	tests := []struct {
		exe     string
		want    string
		wantErr bool
	}{
		{exe: `C:\path with space\Paratech.exe`, want: `"C:\path with space\Paratech.exe" "%1"`},
		{exe: `c:/Users/Иван/AppData/Local/Programs/LearningAssistant/learning-assistant.exe`,
			want: `"c:/Users/Иван/AppData/Local/Programs/LearningAssistant/learning-assistant.exe" "%1"`},
		{exe: `\\server\share\Paratech.exe`, want: `"\\server\share\Paratech.exe" "%1"`},
		{exe: `C:\a"b\Paratech.exe`, wantErr: true},
		{exe: `C:\x" "%1" --data-dir "C:\evil.exe`, wantErr: true},
		{exe: `C:\a%1b\Paratech.exe`, wantErr: true},
		{exe: `C:\%USERPROFILE%\Paratech.exe`, wantErr: true},
		{exe: "C:\\a\nb\\Paratech.exe", wantErr: true},
		{exe: `Paratech.exe`, wantErr: true},
		{exe: `\Paratech.exe`, wantErr: true},
		{exe: `C:Paratech.exe`, wantErr: true},
		{exe: `1:\Paratech.exe`, wantErr: true},
		{exe: `\\`, wantErr: true},
		{exe: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.exe, func(t *testing.T) {
			got, err := Command(tt.exe)
			if tt.wantErr {
				if !errors.Is(err, ErrUnusablePath) || got != "" {
					t.Fatalf("Command = %q, %v; want ErrUnusablePath", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Command = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// fakeKeys are registry values by path and name.
type fakeKeys struct {
	values  map[[2]string]string
	writes  int
	readErr error
	setErr  error
}

func (f *fakeKeys) value(path, name string) (string, bool, error) {
	if f.readErr != nil {
		return "", false, f.readErr
	}
	v, ok := f.values[[2]string{path, name}]
	return v, ok, nil
}

func (f *fakeKeys) setValue(path, name, v string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.writes++
	f.values[[2]string{path, name}] = v
	return nil
}

func TestEnsure(t *testing.T) {
	const exe = `C:\path with space\Paratech.exe`
	const cmdKey = classesKey + `\shell\open\command`
	registered := map[[2]string]string{
		{classesKey, ""}:             "URL:Paratech",
		{classesKey, "URL Protocol"}: "",
		{cmdKey, ""}:                 `"C:\path with space\Paratech.exe" "%1"`,
	}
	with := func(k [2]string, v string, drop bool) map[[2]string]string {
		m := maps.Clone(registered)
		if drop {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name      string
		start     map[[2]string]string
		wantWrite bool
	}{
		{name: "missing", start: map[[2]string]string{}, wantWrite: true},
		{name: "correct", start: registered},
		{name: "another exe", start: with([2]string{cmdKey, ""}, `"C:\Old\Paratech.exe" "%1"`, false), wantWrite: true},
		{name: "unquoted command", start: with([2]string{cmdKey, ""}, `C:\path with space\Paratech.exe %1`, false), wantWrite: true},
		{name: "no command", start: with([2]string{cmdKey, ""}, "", true), wantWrite: true},
		{name: "not a protocol", start: with([2]string{classesKey, "URL Protocol"}, "", true), wantWrite: true},
		{name: "other description", start: with([2]string{classesKey, ""}, "URL:Other", false), wantWrite: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeKeys{values: maps.Clone(tt.start)}
			r := &Registrar{keys: f, root: classesKey}
			wrote, err := r.Ensure(exe)
			if err != nil || wrote != tt.wantWrite {
				t.Fatalf("Ensure = %v, %v; want %v", wrote, err, tt.wantWrite)
			}
			if !maps.Equal(f.values, registered) {
				t.Fatalf("values %q, want %q", f.values, registered)
			}
			if !tt.wantWrite && f.writes != 0 {
				t.Fatalf("%d writes to a correct registration", f.writes)
			}
		})
	}
}

func TestEnsureFailures(t *testing.T) {
	boom := errors.New("access denied")
	tests := []struct {
		name string
		exe  string
		keys *fakeKeys
		want error
	}{
		{name: "quote in the path", exe: `C:\a"b\Paratech.exe`, keys: &fakeKeys{values: map[[2]string]string{}}, want: ErrUnusablePath},
		{name: "read fails", exe: `C:\Paratech.exe`, keys: &fakeKeys{values: map[[2]string]string{}, readErr: boom}, want: ErrRegistry},
		{name: "write fails", exe: `C:\Paratech.exe`, keys: &fakeKeys{values: map[[2]string]string{}, setErr: boom}, want: ErrRegistry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrote, err := (&Registrar{keys: tt.keys, root: classesKey}).Ensure(tt.exe)
			if wrote || !errors.Is(err, tt.want) {
				t.Fatalf("Ensure = %v, %v; want %v", wrote, err, tt.want)
			}
			if len(tt.keys.values) != 0 {
				t.Fatalf("values written: %q", tt.keys.values)
			}
		})
	}
}
