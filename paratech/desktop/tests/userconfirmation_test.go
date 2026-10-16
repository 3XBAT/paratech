package tests

// Source test for the user's confirmation (AC-14, AC-15, TASK-040 follow-up closed by TASK-045): the use case methods
// that act on the user's decision, homeworkflow.Service.Confirm and Execute (an email, an export, a calendar event)
// and senddelivery.Service.MarkSent and SendAgain (the resolution of an unknown outcome), are called in production
// code only by the Wails bindings, where a click of the user is the caller. go/parser has no types, so the check
// is by name: outside the bindings, the use cases that declare these methods and the aggregates that share their
// names, no production file may select a method with one of these names. A false positive (for example
// text/template's Execute in an adapter) is resolved by renaming the call site's wrapper, never by widening the list.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// userOnlyMethods are the method names only a user's action may call.
var userOnlyMethods = map[string]bool{"Confirm": true, "Execute": true, "MarkSent": true, "SendAgain": true}

// userOnlyCallers are the module directories (slash-separated prefixes) where the names may appear.
var userOnlyCallers = []string{
	"internal/input/wails/",        // the bindings: ConfirmSend, CancelSend, MarkSent, SendAgain
	"internal/cases/homeworkflow/", // declares Confirm and Execute
	"internal/cases/senddelivery/", // declares MarkSent and SendAgain and calls the aggregate's
	"internal/domain/",             // the aggregates' own methods (proposal.Confirm, delivery.MarkSent, ...)
}

// checkUserOnlyCalls reports every selector of a user-only method name in a production file outside userOnlyCallers.
// Tests, testenv and tests/ are not production code.
func checkUserOnlyCalls(root string) ([]string, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range files {
		if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "testenv/") || strings.HasPrefix(rel, "tests/") ||
			allowedCaller(rel) {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && userOnlyMethods[sel.Sel.Name] {
				out = append(out, fmt.Sprintf("%s:%d: %s may be called only by a user's action in internal/input/wails (AC-14)",
					rel, fset.Position(sel.Pos()).Line, sel.Sel.Name))
			}
			return true
		})
	}
	return out, nil
}

func allowedCaller(rel string) bool {
	for _, p := range userOnlyCallers {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

func TestUserOnlyMethodsAreCalledOnlyByTheBindings(t *testing.T) {
	violations, err := checkUserOnlyCalls(moduleRoot(t))
	report(t, violations, err)
}

// Control for the checker: a call, a method value and a method expression outside the bindings are found; the
// bindings, the use cases and test files are not reported.
func TestCheckUserOnlyCallsDetectsCallers(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/app/bridge.go":           "package app\n\nfunc f(s svc) {\n\t_, _ = s.Execute(nil, \"p\")\n\tg := s.Confirm\n\t_ = g\n}\n",
		"internal/app/resolve.go":          "package app\n\nvar h = (*svc).SendAgain\n",
		"cmd/desktop/main.go":              "package main\n\nfunc main() { d.MarkSent(nil, \"d\") }\n",
		"internal/app/ok_test.go":          "package app\n\nfunc t(s svc) { s.Execute(nil, \"p\") }\n",
		"internal/input/wails/homework.go": "package wails\n\nfunc c(s svc) { s.Confirm(nil, \"p\"); s.Execute(nil, \"p\") }\n",
		"internal/cases/senddelivery/s.go": "package senddelivery\n\nfunc c(d dl) { d.SendAgain(now) }\n",
		"testenv/fake.go":                  "package testenv\n\nfunc c(s svc) { s.MarkSent(nil, \"d\") }\n",
	})
	got, err := checkUserOnlyCalls(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"internal/app/bridge.go:4: Execute", "internal/app/bridge.go:5: Confirm", "internal/app/resolve.go:3: SendAgain",
		"cmd/desktop/main.go:3: MarkSent",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("not reported: %s\ngot:\n%s", want, joined)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %d violations, want 4:\n%s", len(got), joined)
	}
}
