package tests

// Architecture test (ARCH-64). It enforces docs/architecture.md with go/parser only (no dependencies):
//   - the import matrix of section 9 (ARCH-10, 11, 20, 30, 36, 40, 51, 52);
//   - no time.Now / math/rand in domain and cases (ARCH-15, ARCH-24);
//   - the directory layout of section 2 (ARCH-01);
//   - no organization files (docs, tasks, prompts, ...) inside the code directory paratech/.
//
// The checks are plain functions over a root directory, so TestCheckersDetectViolations can feed them
// synthetic violating trees. The same logic lives in desktop/tests/arch_test.go (duplication is intended:
// no shared test module). Keep the two copies in sync; the desktop copy additionally checks "no import C".

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// modulePath is the module under test; TestRealTree verifies that go.mod agrees.
const modulePath = "paratech/hub"

// --- classification -------------------------------------------------------------------------------

type layer string

const (
	layerRoot     layer = "root" // files in the module root (doc.go)
	layerDomain   layer = "domain"
	layerCases    layer = "cases"
	layerAdapters layer = "adapters"
	layerInput    layer = "input"
	layerApp      layer = "app"
	layerMetrics  layer = "metrics"
	layerConfig   layer = "config"
	layerCmd      layer = "cmd"
	layerTestenv  layer = "testenv"
	layerTests    layer = "tests"
	layerUnknown  layer = "unknown" // internal/<something not in section 2>, reported by checkLayout
)

// pkgRef identifies a package by layer and unit (bounded context, use case, adapter or transport name).
// The domain shared kernel has unit "shared"; a layer root has an empty unit.
type pkgRef struct {
	layer layer
	unit  string
}

// classify maps a package directory relative to the module root (slash separated, "." for the root).
func classify(rel string) pkgRef {
	parts := strings.Split(rel, "/")
	unit := func(i int) string {
		if len(parts) > i {
			return parts[i]
		}
		return ""
	}
	switch parts[0] {
	case "internal":
		switch unit(1) {
		case "domain":
			return pkgRef{layerDomain, unit(2)}
		case "cases":
			return pkgRef{layerCases, unit(2)}
		case "adapters":
			return pkgRef{layerAdapters, unit(2)}
		case "input":
			return pkgRef{layerInput, unit(2)}
		case "app":
			return pkgRef{layerApp, ""}
		case "metrics":
			return pkgRef{layerMetrics, ""}
		}
		return pkgRef{layerUnknown, unit(1)}
	case "config":
		return pkgRef{layerConfig, ""}
	case "cmd":
		return pkgRef{layerCmd, unit(1)}
	case "testenv":
		return pkgRef{layerTestenv, ""}
	case "tests":
		return pkgRef{layerTests, ""}
	}
	return pkgRef{layerRoot, ""}
}

type importKind int

const (
	kindStd    importKind = iota
	kindThird             // third-party
	kindShared            // the shared module (shared/gen contracts)
	kindOther             // a sibling service module (desktop <-> hub)
	kindOwn               // a package of this module
)

func classifyImport(modPath, imp string) (importKind, pkgRef) {
	if imp == modPath {
		return kindOwn, classify(".")
	}
	if rel, ok := strings.CutPrefix(imp, modPath+"/"); ok {
		return kindOwn, classify(rel)
	}
	project, _, _ := strings.Cut(modPath, "/")
	sharedPath := project + "/shared"
	if imp == sharedPath || strings.HasPrefix(imp, sharedPath+"/") {
		return kindShared, pkgRef{}
	}
	if imp == project || strings.HasPrefix(imp, project+"/") {
		return kindOther, pkgRef{}
	}
	first, _, _ := strings.Cut(imp, "/")
	if strings.Contains(first, ".") {
		return kindThird, pkgRef{}
	}
	return kindStd, pkgRef{} // includes the pseudo-package "C", checked separately
}

// importViolation applies the matrix of docs/architecture.md section 9. It returns an empty rule when the import is allowed.
func importViolation(modPath string, src pkgRef, isTest bool, imp string) (rule, why string) {
	kind, dst := classifyImport(modPath, imp)
	switch kind {
	case kindStd:
		return "", ""
	case kindOther:
		return "section-9", "a service must not import a sibling service module"
	}

	switch src.layer {
	case layerDomain:
		switch kind {
		case kindThird:
			return "ARCH-10", "domain must not use third-party packages (exceptions only via ADR)"
		case kindShared:
			return "ARCH-36", "domain must not import shared/gen contracts"
		}
		if dst.layer != layerDomain {
			return "ARCH-10", fmt.Sprintf("domain must not import the %s layer", dst.layer)
		}
		if dst.unit == "shared" {
			return "", ""
		}
		if src.unit == "shared" || src.unit == "" {
			return "ARCH-10", "domain/shared and the domain root may import only domain/shared"
		}
		if dst.unit == "" {
			return "ARCH-10", "a context must not import the domain root package"
		}
		if dst.unit != src.unit {
			return "ARCH-11", fmt.Sprintf("context %q must not import context %q", src.unit, dst.unit)
		}
	case layerCases:
		switch kind {
		case kindThird:
			return "ARCH-20", "cases must not use third-party packages (log/slog is std)"
		case kindShared:
			return "ARCH-36", "cases must not import shared/gen contracts"
		}
		switch dst.layer {
		case layerDomain, layerMetrics:
		case layerCases:
			if src.unit == "" || dst.unit != src.unit {
				return "ARCH-20", fmt.Sprintf("use case %q must not import use case %q", src.unit, dst.unit)
			}
		case layerTestenv:
			if !isTest {
				return "ARCH-20", "only test files of cases may import testenv"
			}
		default:
			return "ARCH-20", fmt.Sprintf("cases must not import the %s layer", dst.layer)
		}
	case layerAdapters:
		if kind != kindOwn {
			return "", "" // third-party and shared/gen are allowed
		}
		switch dst.layer {
		case layerDomain, layerMetrics, layerTestenv:
		case layerAdapters:
			if src.unit == "" || dst.unit != src.unit {
				return "ARCH-30", fmt.Sprintf("adapter %q must not import adapter %q", src.unit, dst.unit)
			}
		case layerCases, layerInput, layerApp:
			return "ARCH-30", fmt.Sprintf("adapters must not import the %s layer", dst.layer)
		}
	case layerInput:
		if kind != kindOwn {
			return "", "" // third-party and shared/gen are allowed
		}
		switch dst.layer {
		case layerDomain, layerCases, layerMetrics, layerTestenv:
		case layerInput:
			if src.unit == "" || dst.unit != src.unit {
				return "ARCH-40", fmt.Sprintf("transport %q must not import transport %q", src.unit, dst.unit)
			}
		case layerAdapters, layerApp:
			return "ARCH-40", fmt.Sprintf("input must not import the %s layer", dst.layer)
		}
	case layerConfig:
		if kind == kindOwn && dst.layer == layerConfig {
			return "", "" // external test package config_test importing config
		}
		return "ARCH-52", "config may depend only on the standard library"
	case layerCmd:
		if kind == kindOwn && (dst.layer == layerConfig || dst.layer == layerApp) {
			return "", ""
		}
		if kind == kindThird {
			return "", ""
		}
		return "ARCH-51", "cmd may import only config and internal/app (plus std and third-party flag/signal helpers)"
	}
	return "", ""
}

// --- file walking ---------------------------------------------------------------------------------

func skipDir(name string) bool { return name == ".git" || name == "node_modules" }

// goFiles returns slash-separated paths (relative to root) of all .go files under root.
func goFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

func parseFile(root, rel string, mode parser.Mode) (*ast.File, error) {
	return parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(rel)), nil, mode)
}

func importPaths(f *ast.File) []string {
	var out []string
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err == nil {
			out = append(out, p)
		}
	}
	return out
}

func dirOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}

func violation(rule, file, msg string) string {
	return fmt.Sprintf("%s: %s: %s", rule, file, msg)
}

// --- checks ---------------------------------------------------------------------------------------

// checkImports enforces the import matrix for every .go file (including tests) of the module at root.
func checkImports(root, modPath string) ([]string, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range files {
		f, err := parseFile(root, rel, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		src := classify(dirOf(rel))
		isTest := strings.HasSuffix(rel, "_test.go")
		for _, imp := range importPaths(f) {
			if rule, why := importViolation(modPath, src, isTest, imp); rule != "" {
				out = append(out, violation(rule, rel, fmt.Sprintf("imports %q: %s", imp, why)))
			}
		}
	}
	return out, nil
}

var clockFuncs = map[string]bool{"Now": true, "Since": true, "Until": true}

var randomPackages = map[string]bool{"math/rand": true, "math/rand/v2": true, "crypto/rand": true}

// checkDeterminism forbids reading the clock and randomness in domain (ARCH-15) and cases (ARCH-24):
// time and IDs come in through parameters and ports.
func checkDeterminism(root string) ([]string, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range files {
		var rule string
		switch classify(dirOf(rel)).layer {
		case layerDomain:
			rule = "ARCH-15"
		case layerCases:
			rule = "ARCH-24"
		default:
			continue
		}
		f, err := parseFile(root, rel, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		timeName := ""
		for _, spec := range f.Imports {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if randomPackages[p] {
				out = append(out, violation(rule, rel, fmt.Sprintf("imports %q: randomness must come through a port or a parameter", p)))
			}
			if p == "time" {
				timeName = "time"
				if spec.Name != nil {
					timeName = spec.Name.Name
				}
			}
		}
		if timeName == "" || timeName == "_" || timeName == "." {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == timeName && clockFuncs[sel.Sel.Name] {
				out = append(out, violation(rule, rel, fmt.Sprintf("uses time.%s: time must come through a Clock port or a parameter", sel.Sel.Name)))
			}
			return true
		})
	}
	return out, nil
}

var internalLayers = map[string]bool{"domain": true, "cases": true, "adapters": true, "input": true, "app": true, "metrics": true}

// checkLayout verifies the directory layout of docs/architecture.md section 2 (ARCH-01):
// every layer has a doc.go, cmd/<binary>/main.go exists, internal/ has no unknown top-level directory
// and every bounded context has a doc.go.
func checkLayout(root string) []string {
	var out []string
	exists := func(rel string) bool {
		st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		return err == nil && !st.IsDir()
	}
	for _, rel := range []string{
		"config/doc.go", "testenv/doc.go", "tests/doc.go",
		"internal/domain/doc.go", "internal/domain/shared/doc.go", "internal/cases/doc.go",
		"internal/adapters/doc.go", "internal/input/doc.go", "internal/app/doc.go", "internal/metrics/doc.go",
	} {
		if !exists(rel) {
			out = append(out, violation("ARCH-01", rel, "required layer file is missing"))
		}
	}

	bins, _ := os.ReadDir(filepath.Join(root, "cmd"))
	hasMain := false
	for _, b := range bins {
		if b.IsDir() && exists("cmd/"+b.Name()+"/main.go") {
			hasMain = true
		}
	}
	if !hasMain {
		out = append(out, violation("ARCH-01", "cmd", "no cmd/<binary>/main.go found"))
	}

	entries, _ := os.ReadDir(filepath.Join(root, "internal"))
	for _, e := range entries {
		if e.IsDir() && !internalLayers[e.Name()] {
			out = append(out, violation("ARCH-01", "internal/"+e.Name(), "unknown top-level directory in internal/ (new ones need an ADR)"))
		}
	}
	contexts, _ := os.ReadDir(filepath.Join(root, "internal", "domain"))
	for _, c := range contexts {
		if c.IsDir() && c.Name() != "shared" && !exists("internal/domain/"+c.Name()+"/doc.go") {
			out = append(out, violation("ARCH-01", "internal/domain/"+c.Name(), "bounded context has no doc.go (purpose and glossary)"))
		}
	}
	return out
}

var (
	orgFiles = map[string]bool{"tasks.json": true, "progress.txt": true, "ralph.sh": true, "CLAUDE.md": true}
	orgDirs  = map[string]bool{"docs": true, "prd": true, "prompts": true, "tasks": true, "adr": true}
)

// checkNoOrgFiles verifies that the code directory (the one holding go.work) contains no files that
// organize the work (docs, tasks, prompts, agent definitions); they live in the repository root.
// Directory names are checked only near the top (paratech/<x> and paratech/<module>/<x>) so that
// legitimate nested packages are not flagged; file names and .claude are checked everywhere.
func checkNoOrgFiles(codeRoot string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(codeRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(codeRoot, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			depth := strings.Count(rel, "/") + 1
			if d.Name() == ".claude" || (depth <= 2 && orgDirs[d.Name()]) {
				out = append(out, violation("org-files", rel, "organization directory inside the code directory"))
				return filepath.SkipDir
			}
			return nil
		}
		if orgFiles[d.Name()] || strings.HasSuffix(d.Name(), ".prompt.md") {
			out = append(out, violation("org-files", rel, "organization file inside the code directory"))
		}
		return nil
	})
	return out, err
}

// --- locating the real tree -----------------------------------------------------------------------

// moduleRoot is the parent of this tests directory (go test runs with the package directory as cwd).
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}

// codeRoot walks up from the module root to the directory holding go.work.
func codeRoot(t *testing.T) string {
	t.Helper()
	dir := moduleRoot(t)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.work not found above the module root")
		}
		dir = parent
	}
}

func moduleNameFromGoMod(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("no module directive in go.mod")
	return ""
}

func report(t *testing.T, violations []string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(violations)
	for _, v := range violations {
		t.Error(v)
	}
}

// --- tests on the real tree -----------------------------------------------------------------------

func TestRealTree(t *testing.T) {
	root := moduleRoot(t)
	if got := moduleNameFromGoMod(t, root); got != modulePath {
		t.Fatalf("go.mod module = %q, arch_test.go expects %q", got, modulePath)
	}
	t.Run("import matrix", func(t *testing.T) {
		v, err := checkImports(root, modulePath)
		report(t, v, err)
	})
	t.Run("no clock or randomness in domain and cases", func(t *testing.T) {
		v, err := checkDeterminism(root)
		report(t, v, err)
	})
	t.Run("layout", func(t *testing.T) {
		report(t, checkLayout(root), nil)
	})
	t.Run("no organization files in the code directory", func(t *testing.T) {
		v, err := checkNoOrgFiles(codeRoot(t))
		report(t, v, err)
	})
}

// --- negative tests on synthetic trees ------------------------------------------------------------

func goSrc(imports ...string) string {
	var b strings.Builder
	b.WriteString("package x\n\nimport (\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "\t_ %q\n", imp)
	}
	b.WriteString(")\n")
	return b.String()
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestImportMatrix(t *testing.T) {
	const m = modulePath
	tests := []struct {
		name string
		file string
		imps []string
		want string // expected rule; empty means the import is allowed
	}{
		// allowed
		{name: "domain std", file: "internal/domain/quota/account/account.go", imps: []string{"errors", "fmt"}},
		{name: "domain own context", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/domain/quota/values"}},
		{name: "domain shared kernel", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/domain/shared"}},
		{name: "cases domain any context", file: "internal/cases/run/service.go", imps: []string{m + "/internal/domain/quota/account", m + "/internal/domain/access/apikey"}},
		{name: "cases metrics and slog", file: "internal/cases/run/service.go", imps: []string{m + "/internal/metrics", "log/slog"}},
		{name: "cases test uses testenv", file: "internal/cases/run/service_test.go", imps: []string{m + "/testenv"}},
		{name: "adapter third party and gen", file: "internal/adapters/postgres/repo.go", imps: []string{"github.com/x/y", "paratech/shared/gen/paratech/hub/v1", m + "/internal/domain/quota/account"}},
		{name: "adapter own subpackage", file: "internal/adapters/postgres/repo.go", imps: []string{m + "/internal/adapters/postgres/sqlcgen"}},
		{name: "input uses cases", file: "internal/input/connect/api.go", imps: []string{m + "/internal/cases/run", m + "/internal/domain/quota/account", "connectrpc.com/connect"}},
		{name: "app wires everything", file: "internal/app/app.go", imps: []string{m + "/internal/adapters/postgres", m + "/internal/cases/run", m + "/internal/input/connect", m + "/config", "paratech/shared/gen/paratech/hub/v1"}},
		{name: "cmd thin", file: "cmd/hub/main.go", imps: []string{m + "/config", m + "/internal/app", "flag"}},
		{name: "config std", file: "config/config.go", imps: []string{"os", "fmt"}},
		{name: "config external test imports config", file: "config/config_test.go", imps: []string{m + "/config"}},
		{name: "tests unrestricted", file: "tests/flow_test.go", imps: []string{m + "/internal/adapters/postgres", m + "/internal/cases/run"}},

		// violations
		{name: "domain imports adapters", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/adapters/postgres"}, want: "ARCH-10"},
		{name: "domain imports cases", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/cases/run"}, want: "ARCH-10"},
		{name: "domain imports input", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/input/connect"}, want: "ARCH-10"},
		{name: "domain imports metrics", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/metrics"}, want: "ARCH-10"},
		{name: "domain imports config", file: "internal/domain/quota/account/account.go", imps: []string{m + "/config"}, want: "ARCH-10"},
		{name: "domain imports third party", file: "internal/domain/quota/account/account.go", imps: []string{"github.com/google/uuid"}, want: "ARCH-10"},
		{name: "domain imports shared gen", file: "internal/domain/quota/account/account.go", imps: []string{"paratech/shared/gen/paratech/hub/v1"}, want: "ARCH-36"},
		{name: "domain imports other context", file: "internal/domain/quota/account/account.go", imps: []string{m + "/internal/domain/access/apikey"}, want: "ARCH-11"},
		{name: "domain test imports testenv", file: "internal/domain/quota/account/account_test.go", imps: []string{m + "/testenv"}, want: "ARCH-10"},
		{name: "shared kernel imports a context", file: "internal/domain/shared/ids.go", imps: []string{m + "/internal/domain/quota"}, want: "ARCH-10"},
		{name: "cases import other use case", file: "internal/cases/run/service.go", imps: []string{m + "/internal/cases/other"}, want: "ARCH-20"},
		{name: "cases import adapters", file: "internal/cases/run/service.go", imps: []string{m + "/internal/adapters/postgres"}, want: "ARCH-20"},
		{name: "cases import input", file: "internal/cases/run/service.go", imps: []string{m + "/internal/input/connect"}, want: "ARCH-20"},
		{name: "cases import app", file: "internal/cases/run/service.go", imps: []string{m + "/internal/app"}, want: "ARCH-20"},
		{name: "cases import third party", file: "internal/cases/run/service.go", imps: []string{"github.com/x/y"}, want: "ARCH-20"},
		{name: "cases import shared gen", file: "internal/cases/run/service.go", imps: []string{"paratech/shared/gen/paratech/hub/v1"}, want: "ARCH-36"},
		{name: "cases non-test imports testenv", file: "internal/cases/run/service.go", imps: []string{m + "/testenv"}, want: "ARCH-20"},
		{name: "adapter imports cases", file: "internal/adapters/postgres/repo.go", imps: []string{m + "/internal/cases/run"}, want: "ARCH-30"},
		{name: "adapter imports input", file: "internal/adapters/postgres/repo.go", imps: []string{m + "/internal/input/connect"}, want: "ARCH-30"},
		{name: "adapter imports app", file: "internal/adapters/postgres/repo.go", imps: []string{m + "/internal/app"}, want: "ARCH-30"},
		{name: "adapter imports other adapter", file: "internal/adapters/postgres/repo.go", imps: []string{m + "/internal/adapters/anthropic"}, want: "ARCH-30"},
		{name: "input imports adapters", file: "internal/input/connect/api.go", imps: []string{m + "/internal/adapters/postgres"}, want: "ARCH-40"},
		{name: "input imports app", file: "internal/input/connect/api.go", imps: []string{m + "/internal/app"}, want: "ARCH-40"},
		{name: "input imports other transport", file: "internal/input/connect/api.go", imps: []string{m + "/internal/input/cli"}, want: "ARCH-40"},
		{name: "config imports domain", file: "config/config.go", imps: []string{m + "/internal/domain/shared"}, want: "ARCH-52"},
		{name: "config imports third party", file: "config/config.go", imps: []string{"github.com/joho/godotenv"}, want: "ARCH-52"},
		{name: "cmd imports domain", file: "cmd/hub/main.go", imps: []string{m + "/internal/domain/shared"}, want: "ARCH-51"},
		{name: "cmd imports adapters", file: "cmd/hub/main.go", imps: []string{m + "/internal/adapters/postgres"}, want: "ARCH-51"},
		{name: "sibling service", file: "internal/app/app.go", imps: []string{"paratech/desktop/internal/domain/shared"}, want: "section-9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{tt.file: goSrc(tt.imps...)})
			got, err := checkImports(root, m)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected no violations, got %v", got)
				}
				return
			}
			if len(got) == 0 || !strings.HasPrefix(got[0], tt.want+": "+tt.file) {
				t.Fatalf("expected a %s violation in %s, got %v", tt.want, tt.file, got)
			}
		})
	}
}

func TestCheckersDetectViolations(t *testing.T) {
	timeNow := "package x\n\nimport \"time\"\n\nfunc f() time.Time { return time.Now() }\n"
	timeAlias := "package x\n\nimport t \"time\"\n\nfunc f() t.Duration { return t.Since(t.Time{}) }\n"
	timeOK := "package x\n\nimport \"time\"\n\nfunc f(d time.Duration, at time.Time) time.Time { return at.Add(d) }\n"

	tests := []struct {
		name  string
		files map[string]string
		check func(t *testing.T, root string) []string
		want  []string // substrings that must all appear (one entry per expected violation); nil means clean
	}{
		{
			name:  "domain time.Now",
			files: map[string]string{"internal/domain/quota/account/account.go": timeNow},
			check: determinism,
			want:  []string{"ARCH-15: internal/domain/quota/account/account.go: uses time.Now"},
		},
		{
			name:  "domain aliased time.Since",
			files: map[string]string{"internal/domain/quota/account/account.go": timeAlias},
			check: determinism,
			want:  []string{"ARCH-15: internal/domain/quota/account/account.go: uses time.Since"},
		},
		{
			name:  "domain time as parameter is fine",
			files: map[string]string{"internal/domain/quota/account/account.go": timeOK},
			check: determinism,
		},
		{
			name:  "domain math/rand",
			files: map[string]string{"internal/domain/quota/account/account.go": goSrc("math/rand")},
			check: determinism,
			want:  []string{`ARCH-15: internal/domain/quota/account/account.go: imports "math/rand"`},
		},
		{
			name:  "domain math/rand/v2",
			files: map[string]string{"internal/domain/quota/account/account.go": goSrc("math/rand/v2")},
			check: determinism,
			want:  []string{`imports "math/rand/v2"`},
		},
		{
			name:  "domain crypto/rand",
			files: map[string]string{"internal/domain/quota/account/account.go": goSrc("crypto/rand")},
			check: determinism,
			want:  []string{`imports "crypto/rand"`},
		},
		{
			name:  "domain test files are checked too",
			files: map[string]string{"internal/domain/quota/account/account_test.go": timeNow},
			check: determinism,
			want:  []string{"ARCH-15: internal/domain/quota/account/account_test.go"},
		},
		{
			name:  "cases time.Now",
			files: map[string]string{"internal/cases/run/service.go": timeNow},
			check: determinism,
			want:  []string{"ARCH-24: internal/cases/run/service.go: uses time.Now"},
		},
		{
			name:  "cases math/rand",
			files: map[string]string{"internal/cases/run/service.go": goSrc("math/rand")},
			check: determinism,
			want:  []string{`ARCH-24: internal/cases/run/service.go: imports "math/rand"`},
		},
		{
			name:  "adapters may use time.Now",
			files: map[string]string{"internal/adapters/clock/clock.go": timeNow},
			check: determinism,
		},
		{
			name:  "layout of an empty tree",
			check: func(t *testing.T, root string) []string { return checkLayout(root) },
			want: []string{
				"ARCH-01: config/doc.go", "ARCH-01: testenv/doc.go", "ARCH-01: tests/doc.go",
				"ARCH-01: internal/domain/doc.go", "ARCH-01: internal/domain/shared/doc.go",
				"ARCH-01: internal/cases/doc.go", "ARCH-01: internal/adapters/doc.go", "ARCH-01: internal/input/doc.go",
				"ARCH-01: internal/app/doc.go", "ARCH-01: internal/metrics/doc.go", "ARCH-01: cmd:",
			},
		},
		{
			name: "layout unknown internal dir and context without doc.go",
			files: map[string]string{
				"config/doc.go": "package config", "testenv/doc.go": "package testenv", "tests/doc.go": "package tests",
				"cmd/hub/main.go":                          "package main",
				"internal/domain/doc.go":                   "package domain",
				"internal/domain/shared/doc.go":            "package shared",
				"internal/domain/quota/account/account.go": "package job",
				"internal/cases/doc.go":                    "package cases",
				"internal/adapters/doc.go":                 "package adapters",
				"internal/input/doc.go":                    "package input",
				"internal/app/doc.go":                      "package app",
				"internal/metrics/doc.go":                  "package metrics",
				"internal/rogue/rogue.go":                  "package rogue",
			},
			check: func(t *testing.T, root string) []string { return checkLayout(root) },
			want:  []string{"ARCH-01: internal/rogue", "ARCH-01: internal/domain/quota: bounded context has no doc.go"},
		},
		{
			name: "organization files",
			files: map[string]string{
				"go.work":                  "go 1.26\n",
				"docs/architecture.md":     "x",
				"tasks.json":               "{}",
				"progress.txt":             "x",
				"ralph.sh":                 "x",
				"CLAUDE.md":                "x",
				"desktop/go.mod":           "module m",
				"desktop/x.prompt.md":      "x",
				".claude/settings.json":    "{}",
				"desktop/prd/prd-v1.md":    "x",
				"hub/frontend/.claude/a":   "x",
				"desktop/internal/ok.go":   "package x",
				"desktop/internal/docs.go": "package x",
			},
			check: func(t *testing.T, root string) []string {
				v, err := checkNoOrgFiles(root)
				if err != nil {
					t.Fatal(err)
				}
				return v
			},
			want: []string{
				"org-files: docs", "org-files: tasks.json", "org-files: progress.txt", "org-files: ralph.sh",
				"org-files: CLAUDE.md", "org-files: desktop/x.prompt.md", "org-files: .claude", "org-files: desktop/prd",
				"org-files: hub/frontend/.claude",
			},
		},
		{
			name: "organization names deep inside code are fine",
			files: map[string]string{
				"go.work":                           "go 1.26\n",
				"desktop/internal/domain/docs/x.go": "package docs",
				"desktop/frontend/node_modules/pkg/docs/readme.md": "x",
			},
			check: func(t *testing.T, root string) []string {
				v, err := checkNoOrgFiles(root)
				if err != nil {
					t.Fatal(err)
				}
				return v
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.check(t, writeTree(t, tt.files))
			sort.Strings(got)
			if tt.want == nil {
				if len(got) != 0 {
					t.Fatalf("expected a clean tree, got %v", got)
				}
				return
			}
			for _, want := range tt.want {
				found := false
				for _, g := range got {
					if strings.HasPrefix(g, want) || strings.Contains(g, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing violation %q in %v", want, got)
				}
			}
		})
	}
}

func determinism(t *testing.T, root string) []string {
	t.Helper()
	v, err := checkDeterminism(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
