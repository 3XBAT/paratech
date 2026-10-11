package tests

// Contract test for the use case ports (TASK-003, ARCH-21, ARCH-25, ARCH-26, ARCH-36). It parses
// internal/cases/<usecase>/ports.go with go/parser (no dependencies) and checks:
//   - every use case of docs/architecture.md section 12 has a ports.go and a doc.go;
//   - ports.go holds interfaces only (no implementations, no DTO types of the use case);
//   - every interface and every method has a doc comment with a PRD section reference;
//   - only primitives, standard library types and domain types appear in method signatures, and the standard
//     packages that stand for I/O or the OS (os, database/sql, net/http, net, unsafe, io/fs) never do;
//   - methods take context.Context first (ARCH-25), except the pure Clock and IDGenerator ports.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// useCases are the desktop use cases of docs/architecture.md section 12.
var useCases = []string{
	"recordingsession", "jobrunner", "transcribe", "modelcatalog", "schedulesync", "homeworkflow", "senddelivery",
}

// requiredPorts lists the ports TASK-003 must declare, and in which use case at least one declares them.
var requiredPorts = map[string][]string{
	"RecordingRepository":  {"recordingsession"},
	"RecordingTranscripts": {"recordingsession"},
	"JobRepository":        {"jobrunner"},
	"TranscriptRepository": {"transcribe"},
	"ModelRepository":      {"modelcatalog"},
	"ModelPins":            {"modelcatalog"},
	"LessonRepository":     {"schedulesync"},
	"AssignmentRepository": {"homeworkflow"},
	"DeliveryRepository":   {"senddelivery"},
	"SettingsReader":       {"recordingsession", "jobrunner", "transcribe"},
	"SettingsRepository":   {"modelcatalog"},
	"Clock":                {"recordingsession", "jobrunner", "transcribe", "modelcatalog", "schedulesync", "homeworkflow", "senddelivery"},
	"IDGenerator":          {"recordingsession", "transcribe", "modelcatalog", "schedulesync", "homeworkflow", "senddelivery"},
	"Recorder":             {"recordingsession"},
	"STTEngine":            {"transcribe"},
	"PowerMonitor":         {"jobrunner", "transcribe"},
	"CredentialStore":      {"senddelivery"},
	"ArtifactStore":        {"recordingsession", "transcribe", "modelcatalog", "homeworkflow", "senddelivery"},
	"CalendarAdapter":      {"schedulesync"},
	"EmailAdapter":         {"senddelivery"},
	"LLMClient":            {"homeworkflow"},
	"Handler":              {"jobrunner"},
	"Tx":                   {"recordingsession", "jobrunner", "transcribe", "modelcatalog", "schedulesync", "homeworkflow", "senddelivery"},
}

var prdRef = regexp.MustCompile(`PRD sections? \d`)

// noContextPorts are pure in-memory ports that need no context (ARCH-24).
var noContextPorts = map[string]bool{"Clock": true, "IDGenerator": true}

// deniedStd are standard packages whose types must not appear in port signatures: they would tie the core to
// the OS, the database or the network (hard rule 5, ARCH-26). io, context and time stay allowed.
var deniedStd = map[string]bool{
	"os": true, "database/sql": true, "net/http": true, "net": true, "unsafe": true, "io/fs": true,
}

var builtinTypes = map[string]bool{
	"string": true, "bool": true, "error": true, "int": true, "int64": true, "byte": true,
}

const domainImportPrefix = modulePath + "/internal/domain/"

type portIssue struct{ file, msg string }

// portTypePackageAllowed reports whether types of the import path may appear in a port: the standard library
// (the first element has no dot, and it is not this module) or a domain package.
func portTypePackageAllowed(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	isStd := !strings.Contains(first, ".") && first != "paratech"
	return isStd || strings.HasPrefix(path, domainImportPrefix)
}

// checkPortsFile applies the rules above to one ports.go and returns the interface names it declares.
func checkPortsFile(file string) (names []string, issues []portIssue) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, []portIssue{{file, err.Error()}}
	}
	bad := func(format string, args ...any) {
		issues = append(issues, portIssue{file, fmt.Sprintf(format, args...)})
	}

	// import name (the alias, or the last path element) -> import path
	imports := map[string]string{}
	for _, spec := range f.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = p
		if !portTypePackageAllowed(p) {
			bad("imports %q: only std and domain types may appear in ports (ARCH-26, ARCH-36)", p)
		}
		if deniedStd[p] {
			bad("imports %q: this standard package must not appear in ports (hard rule 5, ARCH-26)", p)
		}
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			bad("declares func %s: ports.go holds interfaces only, no implementations", d.Name.Name)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts := spec.(*ast.TypeSpec)
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					bad("type %s is not an interface: DTOs of the use case must not cross a port (ARCH-26)", ts.Name.Name)
					continue
				}
				names = append(names, ts.Name.Name)
				doc := ts.Doc
				if doc == nil {
					doc = d.Doc
				}
				if doc == nil || !prdRef.MatchString(doc.Text()) {
					bad("interface %s has no doc comment with a PRD section reference", ts.Name.Name)
				}
				for _, m := range it.Methods.List {
					checkMethod(ts.Name.Name, m, imports, bad)
				}
			}
		}
	}
	return names, issues
}

func checkMethod(iface string, m *ast.Field, imports map[string]string, bad func(string, ...any)) {
	if len(m.Names) != 1 {
		bad("%s embeds an interface or has an unnamed method; ports list their methods explicitly", iface)
		return
	}
	name := iface + "." + m.Names[0].Name
	ft, ok := m.Type.(*ast.FuncType)
	if !ok {
		bad("%s is not a method", name)
		return
	}
	if m.Doc == nil || !prdRef.MatchString(m.Doc.Text()) {
		bad("method %s has no doc comment with a PRD section reference", name)
	}

	if !noContextPorts[iface] {
		if ft.Params == nil || len(ft.Params.List) == 0 || !isContext(ft.Params.List[0].Type) {
			bad("method %s must take context.Context first (ARCH-25)", name)
		}
	}

	check := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			checkType(name, field.Type, imports, bad)
		}
	}
	check(ft.Params)
	check(ft.Results)
}

func isContext(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "context" && sel.Sel.Name == "Context"
}

// checkType walks a type expression: bare identifiers must be builtins, qualified ones must come from
// an allowed (std or domain) package that is not on the deny list. A bare non-builtin identifier would be a
// type of the use case itself.
func checkType(method string, e ast.Expr, imports map[string]string, bad func(string, ...any)) {
	switch t := e.(type) {
	case *ast.Ident:
		if !builtinTypes[t.Name] {
			bad("method %s uses type %s that is neither a primitive nor a qualified domain/std type (ARCH-26)", method, t.Name)
		}
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok {
			bad("method %s uses an unsupported qualified type", method)
			return
		}
		path, imported := imports[x.Name]
		switch {
		case !imported:
			bad("method %s uses qualifier %s that is not imported", method, x.Name)
		case deniedStd[path]:
			bad("method %s uses a type from the denied standard package %q (hard rule 5, ARCH-26)", method, path)
		case !portTypePackageAllowed(path):
			bad("method %s uses a type from package %q that is not std or domain (ARCH-26, ARCH-36)", method, path)
		}
	case *ast.StarExpr:
		checkType(method, t.X, imports, bad)
	case *ast.ArrayType:
		checkType(method, t.Elt, imports, bad)
	case *ast.Ellipsis:
		checkType(method, t.Elt, imports, bad)
	case *ast.ChanType:
		checkType(method, t.Value, imports, bad)
	case *ast.FuncType:
		for _, fl := range []*ast.FieldList{t.Params, t.Results} {
			if fl == nil {
				continue
			}
			for _, field := range fl.List {
				checkType(method, field.Type, imports, bad)
			}
		}
	default:
		bad("method %s uses an unsupported type expression %T", method, e)
	}
}

func TestUseCasePorts(t *testing.T) {
	root := moduleRoot(t)
	declared := map[string][]string{} // port name -> use cases declaring it

	for _, uc := range useCases {
		dir := filepath.Join(root, "internal", "cases", uc)
		for _, name := range []string{"doc.go", "ports.go"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("use case %s: missing %s", uc, name)
			}
		}
		names, issues := checkPortsFile(filepath.Join(dir, "ports.go"))
		for _, is := range issues {
			t.Errorf("%s: %s", filepath.ToSlash(strings.TrimPrefix(is.file, root)), is.msg)
		}
		for _, n := range names {
			declared[n] = append(declared[n], uc)
		}
	}

	for port, want := range requiredPorts {
		got := declared[port]
		sort.Strings(got)
		w := append([]string(nil), want...)
		sort.Strings(w)
		for _, uc := range w {
			if !contains(got, uc) {
				t.Errorf("port %s is not declared in use case %s", port, uc)
			}
		}
	}
}

// TestPortsCheckerDetectsViolations feeds the checker a synthetic ports.go that breaks every rule. Each
// expectation names the method it belongs to, so a violation cannot be "found" for another reason.
func TestPortsCheckerDetectsViolations(t *testing.T) {
	src := `package x

import (
	"context"
	"database/sql"
	"io/fs"
	"net"
	"net/http"
	"os"
	"unsafe"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/desktop/internal/cases/other"
)

// Undocumented has no PRD reference.
type Undocumented interface {
	// Do has no reference either.
	Do(ctx context.Context) error
}

// Dto is a struct (PRD section 1).
type Dto struct{}

// Leaky takes use case types (PRD section 1).
type Leaky interface {
	// Local uses a local type (PRD section 1).
	Local(ctx context.Context, d Dto) error
	// Foreign uses a generated type (PRD section 1).
	Foreign(ctx context.Context, r hubv1.Request) error
	// Ghost uses a qualifier that is not imported (PRD section 1).
	Ghost(ctx context.Context, r ghost.Thing) error
	// NoCtx forgot the context (PRD section 1).
	NoCtx(id string) error
}

// Denied uses standard packages that stand for I/O (PRD section 1).
type Denied interface {
	// OS takes a file (PRD section 1).
	OS(ctx context.Context, f *os.File) error
	// SQL takes a database handle (PRD section 1).
	SQL(ctx context.Context, db *sql.DB) error
	// HTTP takes a client (PRD section 1).
	HTTP(ctx context.Context, c *http.Client) error
	// Net takes a connection (PRD section 1).
	Net(ctx context.Context, c net.Conn) error
	// Unsafe takes a pointer (PRD section 1).
	Unsafe(ctx context.Context, p unsafe.Pointer) error
	// FS takes a file system (PRD section 1).
	FS(ctx context.Context, f fs.FS) error
}

func Impl() {}
`
	issues := checkSource(t, src)
	for _, want := range []string{
		`imports "paratech/shared/gen/paratech/hub/v1"`,
		`imports "paratech/desktop/internal/cases/other"`,
		`imports "os": this standard package must not appear in ports`,
		`imports "database/sql": this standard package`,
		`imports "net/http": this standard package`,
		`imports "net": this standard package`,
		`imports "unsafe": this standard package`,
		`imports "io/fs": this standard package`,
		"interface Undocumented has no doc comment",
		"method Undocumented.Do has no doc comment",
		"type Dto is not an interface",
		"method Leaky.Local uses type Dto",
		`method Leaky.Foreign uses a type from package "paratech/shared/gen/paratech/hub/v1" that is not std or domain`,
		"method Leaky.Ghost uses qualifier ghost that is not imported",
		"method Leaky.NoCtx must take context.Context first",
		`method Denied.OS uses a type from the denied standard package "os"`,
		`method Denied.SQL uses a type from the denied standard package "database/sql"`,
		`method Denied.HTTP uses a type from the denied standard package "net/http"`,
		`method Denied.Net uses a type from the denied standard package "net"`,
		`method Denied.Unsafe uses a type from the denied standard package "unsafe"`,
		`method Denied.FS uses a type from the denied standard package "io/fs"`,
		"declares func Impl",
	} {
		if !strings.Contains(issues, want) {
			t.Errorf("checker did not report %q; got:\n%s", want, issues)
		}
	}
}

// TestPortsCheckerAcceptsValidFile is the control for the test above: a clean ports.go yields no issues, so
// violations are reported for the right reason and allowed packages (io, time, domain) are not rejected.
func TestPortsCheckerAcceptsValidFile(t *testing.T) {
	src := `package x

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Store keeps things (PRD section 1).
type Store interface {
	// Open reads an artifact (PRD section 1).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
	// Touch uses time and a callback (PRD section 1).
	Touch(ctx context.Context, at time.Time, progress func(percent int)) error
}
`
	if issues := checkSource(t, src); issues != "" {
		t.Fatalf("valid ports.go reported issues:\n%s", issues)
	}
}

// checkSource writes src as ports.go in a temp dir, runs the checker and joins the messages.
func checkSource(t *testing.T, src string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "ports.go")
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	_, issues := checkPortsFile(file)
	var all []string
	for _, is := range issues {
		all = append(all, is.msg)
	}
	return strings.Join(all, "\n")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
