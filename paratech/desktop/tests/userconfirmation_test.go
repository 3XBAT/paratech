package tests

// Source test for the user's confirmation (AC-14, AC-15, TASK-040 follow-up closed by TASK-045): the use case methods
// that act on the user's decision, homeworkflow.Service.Confirm, Execute, ConfirmSend and ConfirmDeadline (an email,
// an export, a calendar event; ConfirmDeadline since TASK-127) and senddelivery.Service.MarkSent and SendAgain (the resolution of an unknown outcome), are called in
// production code only by the Wails bindings, where a click of the user is the caller. go/parser has no types, so the
// check is by name: no production file may select a method with one of these names, except the bindings and the few
// use case files that implement these methods on top of the aggregates' methods of the same names. The list names
// files, not packages, so the code that handles model output (postprocess.go, drafts.go) cannot call them. A false
// positive (for example text/template's Execute in an adapter) is resolved by renaming the call site's wrapper, never
// by widening the list.

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
var userOnlyMethods = map[string]bool{
	"Confirm": true, "Execute": true, "ConfirmSend": true, "ConfirmDeadline": true, "MarkSent": true, "SendAgain": true,
}

// userOnlyCallers are the production files (module-relative, slash-separated) where the names may appear.
var userOnlyCallers = map[string]bool{
	"internal/input/wails/homework.go":         true, // the bindings: ConfirmSend, MarkSent, SendAgain
	"internal/input/wails/agentconfirm.go":     true, // «Да» of the agent's confirm card: ConfirmSend, ConfirmDeadline
	"internal/cases/homeworkflow/proposals.go": true, // ConfirmSend = proposal.Confirm, then Execute
	"internal/cases/senddelivery/service.go":   true, // MarkSent and SendAgain on the delivery aggregate
	"internal/cases/senddelivery/job.go":       true, // the reconciler's resolution on the delivery aggregate
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

func allowedCaller(rel string) bool { return userOnlyCallers[rel] }

func TestUserOnlyMethodsAreCalledOnlyByTheBindings(t *testing.T) {
	violations, err := checkUserOnlyCalls(moduleRoot(t))
	report(t, violations, err)
}

// Control for the checker: a call, a method value and a method expression outside the listed files are found, also in
// another file of a listed package (the model output handling of homeworkflow); the listed files and test files are
// not reported.
func TestCheckUserOnlyCallsDetectsCallers(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/app/bridge.go":                   "package app\n\nfunc f(s svc) {\n\t_, _ = s.Execute(nil, \"p\")\n\tg := s.Confirm\n\t_ = g\n}\n",
		"internal/app/resolve.go":                  "package app\n\nvar h = (*svc).SendAgain\n",
		"cmd/desktop/main.go":                      "package main\n\nfunc main() { d.MarkSent(nil, \"d\") }\n",
		"internal/cases/homeworkflow/drafts.go":    "package homeworkflow\n\nfunc c(s svc) { s.ConfirmSend(nil, \"p\") }\n",
		"internal/cases/senddelivery/other.go":     "package senddelivery\n\nfunc c(d dl) { d.SendAgain(now) }\n",
		"internal/app/ok_test.go":                  "package app\n\nfunc t(s svc) { s.Execute(nil, \"p\") }\n",
		"internal/input/wails/homework.go":         "package wails\n\nfunc c(s svc) { s.ConfirmSend(nil, \"p\"); s.MarkSent(nil, \"d\") }\n",
		"internal/cases/homeworkflow/proposals.go": "package homeworkflow\n\nfunc c(p pr) { p.Confirm(now); s.Execute(nil, \"p\") }\n",
		"internal/cases/senddelivery/service.go":   "package senddelivery\n\nfunc c(d dl) { d.SendAgain(now) }\n",
		"testenv/fake.go":                          "package testenv\n\nfunc c(s svc) { s.MarkSent(nil, \"d\") }\n",
	})
	got, err := checkUserOnlyCalls(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	want := []string{
		"internal/app/bridge.go:4: Execute", "internal/app/bridge.go:5: Confirm", "internal/app/resolve.go:3: SendAgain",
		"cmd/desktop/main.go:3: MarkSent", "internal/cases/homeworkflow/drafts.go:3: ConfirmSend",
		"internal/cases/senddelivery/other.go:3: SendAgain",
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("not reported: %s\ngot:\n%s", w, joined)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d violations, want %d:\n%s", len(got), len(want), joined)
	}
}
