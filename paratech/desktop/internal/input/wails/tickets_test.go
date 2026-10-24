package wails

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/tickets"
	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/shared"
)

// fakeTickets records the calls and answers with view, or err.
type fakeTickets struct {
	view       tickets.View
	err        error
	calls      []string
	acceptCost bool
}

func (f *fakeTickets) Propose(_ context.Context, sub shared.SubjectID, text string) (tickets.View, error) {
	f.calls = append(f.calls, fmt.Sprintf("Propose %s %q", sub, text))
	return f.view, f.err
}

func (f *fakeTickets) ProposeFromMaterial(_ context.Context, id shared.MaterialID) (tickets.View, error) {
	f.calls = append(f.calls, "ProposeFromMaterial "+string(id))
	return f.view, f.err
}

func (f *fakeTickets) Set(_ context.Context, id shared.TicketSetID) (tickets.View, error) {
	f.calls = append(f.calls, "Set "+string(id))
	return f.view, f.err
}

func (f *fakeTickets) Confirm(_ context.Context, id shared.TicketSetID, acceptCost bool) (tickets.Started, error) {
	f.calls, f.acceptCost = append(f.calls, "Confirm "+string(id)), acceptCost
	return tickets.Started{View: f.view, Job: "j1", Created: true}, f.err
}

func (f *fakeTickets) Cancel(_ context.Context, id shared.TicketSetID) (tickets.View, error) {
	f.calls = append(f.calls, "Cancel "+string(id))
	return f.view, f.err
}

func (f *fakeTickets) Unfile(_ context.Context, id shared.TicketSetID) (tickets.View, error) {
	f.calls = append(f.calls, "Unfile "+string(id))
	return f.view, f.err
}

func TestTicketServiceMapsTheSet(t *testing.T) {
	ref, err := shared.NewArtifactRef(shared.ArtifactMaterials, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeTickets{view: tickets.View{
		ID: "ts1", Subject: "sub1", Status: ticketset.StatusDone, Done: 2, Total: 2,
		Tickets: []tickets.TicketView{
			{Number: 1, Title: "<b>Ряды</b>", Answered: true, Coverage: ticketset.CoverageCovered, Answer: "an1"},
			{Number: 2, Title: "Пределы", Answered: true, Coverage: ticketset.CoverageNotCovered},
		},
		Budget: ticketset.Estimate(2), Report: ticketset.Report{Covered: 1, NotCovered: 1}, Material: "file1", Result: ref,
		Version: 4,
	}}
	s := NewTicketService(f, discard())
	got, err := s.Get(t.Context(), "ts1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "ts1" || got.Status != "done" || len(got.Tickets) != 2 || got.Tickets[0].Title != "<b>Ряды</b>" ||
		got.Tickets[0].AnswerID != "an1" || got.Tickets[1].Coverage != "not_covered" || got.Report.Covered != 1 ||
		got.MaterialID != "file1" || got.Result != ref.String() || got.Budget.Tickets != 2 || got.Job != "" {
		t.Fatalf("set = %+v", got)
	}

	c, err := s.Confirm(t.Context(), "ts1", true)
	if err != nil || c.Job != "j1" || !f.acceptCost {
		t.Fatalf("confirm = %+v, %v", c, err)
	}
	for _, call := range []func() (TicketSet, error){
		func() (TicketSet, error) { return s.Propose(t.Context(), "sub1", "1. Ряды") },
		func() (TicketSet, error) { return s.ProposeFromMaterial(t.Context(), "m1") },
		func() (TicketSet, error) { return s.Cancel(t.Context(), "ts1") },
		func() (TicketSet, error) { return s.Undo(t.Context(), "ts1") },
	} {
		if _, err := call(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{`Set ts1`, `Confirm ts1`, `Propose sub1 "1. Ряды"`, `ProposeFromMaterial m1`, `Cancel ts1`, `Unfile ts1`}
	if fmt.Sprint(f.calls) != fmt.Sprint(want) {
		t.Fatalf("calls %q", f.calls)
	}

	empty, err := NewTicketService(&fakeTickets{}, discard()).Get(t.Context(), "ts1")
	if err != nil || empty.Tickets == nil {
		t.Fatalf("empty lists are []: %+v, %v", empty, err)
	}
}

func TestTicketServiceErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("%w: x", tickets.ErrNotFound), ErrTicketsNotFound},
		{tickets.ErrNoTickets, ErrTicketsNoTickets},
		{tickets.ErrTooManyTickets, ErrTicketsTooMany},
		{tickets.ErrListTooLong, ErrTicketsListTooLong},
		{tickets.ErrTicketTooLong, ErrTicketsTicketTooLong},
		{tickets.ErrNoSubject, ErrTicketsNoSubject},
		{tickets.ErrSubjectNotFound, ErrTicketsSubject},
		{tickets.ErrSourceNotFound, ErrTicketsSource},
		{tickets.ErrNoText, ErrTicketsNoText},
		{tickets.ErrCostNotAccepted, ErrTicketsCost},
		{tickets.ErrAlreadyDone, ErrTicketsDone},
		{tickets.ErrChanged, ErrTicketsChanged},
		{tickets.ErrInvalidRequest, ErrTicketsInvalid},
		{tickets.ErrRetries, ErrTicketsInvalid},
		{fmt.Errorf("%w: disk full", tickets.ErrUnavailable), ErrTicketsUnavailable},
	}
	for _, tt := range tests {
		s := NewTicketService(&fakeTickets{err: tt.err}, discard())
		if _, err := s.Confirm(t.Context(), "ts1", false); !errors.Is(err, tt.want) {
			t.Errorf("%v: Confirm = %v, want %v", tt.err, err, tt.want)
		}
		if _, err := s.Get(t.Context(), "ts1"); !errors.Is(err, tt.want) || err.Error() != tt.want.Error() {
			t.Errorf("%v: Get = %v", tt.err, err)
		}
	}
}
