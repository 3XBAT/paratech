package tests

// Contract test for the side-effect boundary (PRD section 13.3, AC-18, TASK-040; docs/refs/prompt-injection.md):
//   - no side-effect port of any use case takes model output (values.UntrustedOutput or values.LLMResult), not even
//     nested in a struct, slice or pointer;
//   - the side-effect ports of the proposal pipeline take exactly a context and one command of the proposal package,
//     and the command has no exported field, so it can only come from a confirmed proposal.

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
)

// sideEffectPorts are the ports that reach email, files or calendars. Add new ones here.
var sideEffectPorts = []reflect.Type{
	ifaceOf[senddelivery.EmailAdapter](),
	ifaceOf[schedulesync.CalendarAdapter](),
	ifaceOf[homeworkflow.HomeworkMailer](),
	ifaceOf[homeworkflow.HomeworkExporter](),
	ifaceOf[homeworkflow.DeadlineCalendar](),
}

var modelOutputTypes = []reflect.Type{
	reflect.TypeOf(values.UntrustedOutput{}),
	reflect.TypeOf(values.LLMResult{}),
}

// carries reports whether t is or contains one of the model output types.
func carries(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	for _, m := range modelOutputTypes {
		if t == m {
			return true
		}
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return carries(t.Elem(), seen)
	case reflect.Map:
		return carries(t.Key(), seen) || carries(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if carries(t.Field(i).Type, seen) {
				return true
			}
		}
	case reflect.Func:
		for i := range t.NumIn() {
			if carries(t.In(i), seen) {
				return true
			}
		}
	}
	return false
}

func TestSideEffectPortsNeverTakeModelOutput(t *testing.T) {
	for _, port := range sideEffectPorts {
		for i := range port.NumMethod() {
			m := port.Method(i)
			for j := range m.Type.NumIn() {
				if in := m.Type.In(j); carries(in, map[reflect.Type]bool{}) {
					t.Errorf("%s.%s parameter %d (%v) carries model output (AC-18)", port.Name(), m.Name, j, in)
				}
			}
		}
	}
}

func TestProposalPortsTakeOnlyConfirmedCommands(t *testing.T) {
	commands := map[reflect.Type]bool{
		reflect.TypeOf(proposal.EmailCommand{}):    true,
		reflect.TypeOf(proposal.ExportCommand{}):   true,
		reflect.TypeOf(proposal.CalendarCommand{}): true,
	}
	for cmd := range commands {
		for i := range cmd.NumField() {
			if f := cmd.Field(i); f.IsExported() {
				t.Errorf("%v has exported field %s: commands must only come from a confirmed proposal", cmd, f.Name)
			}
		}
	}

	ctxType := ifaceOf[context.Context]()
	for _, port := range []reflect.Type{
		ifaceOf[homeworkflow.HomeworkMailer](), ifaceOf[homeworkflow.HomeworkExporter](), ifaceOf[homeworkflow.DeadlineCalendar](),
	} {
		for i := range port.NumMethod() {
			m := port.Method(i)
			if m.Type.NumIn() != 2 || m.Type.In(0) != ctxType || !commands[m.Type.In(1)] {
				t.Errorf("%s.%s must take (context.Context, a proposal command), got %v", port.Name(), m.Name, m.Type)
			}
		}
	}
}

// Control for the walker: a type that nests model output is detected.
func TestCarriesDetectsNestedModelOutput(t *testing.T) {
	type nested struct {
		Results []*values.LLMResult
	}
	if !carries(reflect.TypeOf(nested{}), map[reflect.Type]bool{}) {
		t.Fatal("nested LLMResult not detected")
	}
	if carries(reflect.TypeOf(proposal.EmailCommand{}), map[reflect.Type]bool{}) {
		t.Fatal("EmailCommand reported as carrying model output")
	}
}

// The post-processing of a transcript (TASK-042) only stores data: none of its ports is a side-effect port, and no
// port it calls takes model output, so the answer of the model reaches the database only as validated aggregate values.
func TestPostProcessorReachesNoSideEffect(t *testing.T) {
	checkNoSideEffectPorts(t, reflect.TypeOf(homeworkflow.PostProcessDeps{}), 10)
	checkNoSideEffectPorts(t, reflect.TypeOf(homeworkflow.RerunDeps{}), 5) // TASK-086: «Повторить» only enqueues jobs
}

// The homework drafts (TASK-043) have no side-effect port either: a generated draft is stored as a version. Exporting
// writes through the Storage Manager port (ArtifactStore), which takes no model output, and only on the user's action.
func TestDraftsReachNoSideEffect(t *testing.T) {
	checkNoSideEffectPorts(t, reflect.TypeOf(homeworkflow.DraftDeps{}), 10)
}

// The shape of the ports of the proposal service (wired by TASK-060): the outside is reached only through the three
// command ports and no port takes model output. That the commands run only after the confirmation is proven by the
// wired tests (internal/app/proposals_test.go).
func TestProposalServiceReachesSideEffectsOnlyThroughCommands(t *testing.T) {
	deps := reflect.TypeOf(homeworkflow.Deps{})
	commandPorts := map[reflect.Type]bool{
		ifaceOf[homeworkflow.HomeworkMailer](): true, ifaceOf[homeworkflow.HomeworkExporter](): true,
		ifaceOf[homeworkflow.DeadlineCalendar](): true,
	}
	found := 0
	for i := range deps.NumField() {
		f := deps.Field(i)
		if f.Type.Kind() != reflect.Interface {
			continue
		}
		if commandPorts[f.Type] {
			found++
		} else if slices.Contains(sideEffectPorts, f.Type) {
			t.Errorf("Deps.%s is the side-effect port %s without a command", f.Name, f.Type.Name())
		}
		for j := range f.Type.NumMethod() {
			m := f.Type.Method(j)
			for k := range m.Type.NumIn() {
				if in := m.Type.In(k); carries(in, map[reflect.Type]bool{}) {
					t.Errorf("%s.%s parameter %d (%v) takes model output", f.Type.Name(), m.Name, k, in)
				}
			}
		}
	}
	if found != len(commandPorts) {
		t.Fatalf("found %d of the %d command ports in homeworkflow.Deps", found, len(commandPorts))
	}
}

// checkNoSideEffectPorts checks that no port field of deps is a side-effect port or takes model output; min guards
// against a walker that misses the fields.
func checkNoSideEffectPorts(t *testing.T, deps reflect.Type, minPorts int) {
	t.Helper()
	ports := 0
	for i := range deps.NumField() {
		f := deps.Field(i)
		if f.Type.Kind() != reflect.Interface {
			continue
		}
		ports++
		for _, se := range sideEffectPorts {
			if f.Type == se {
				t.Errorf("%s.%s is the side-effect port %s (hard rule 3)", deps.Name(), f.Name, se.Name())
			}
		}
		for j := range f.Type.NumMethod() {
			m := f.Type.Method(j)
			for k := range m.Type.NumIn() {
				if in := m.Type.In(k); carries(in, map[reflect.Type]bool{}) {
					t.Errorf("%s.%s parameter %d (%v) takes model output", f.Type.Name(), m.Name, k, in)
				}
			}
		}
	}
	if ports < minPorts {
		t.Fatalf("checked %d ports of %s, the walker missed some", ports, deps.Name())
	}
}
