package agent

import (
	"context"
	"fmt"

	"paratech/desktop/internal/cases/agentturn"
)

const opNewProgress = "agent: new progress"

// Progress is agentturn.Progress over the panel's Sink: before a step goes to the Hub the panel gets the request's
// thread, turn and status (Update.Progress, no cards), so «Стоп» works during the first step of a new request
// (ADR-0026). It needs no Driver, so app wires it into the step use case without a cycle. It runs under the use case's
// lock and only publishes.
type Progress struct {
	sink Sink
}

// NewProgress checks the sink.
func NewProgress(sink Sink) (*Progress, error) {
	if sink == nil {
		return nil, fmt.Errorf("%s: %w: a sink is required", opNewProgress, ErrInvalidConfig)
	}
	return &Progress{sink: sink}, nil
}

// Started publishes the state of the request whose step goes to the Hub.
func (p *Progress) Started(_ context.Context, st agentturn.Step) {
	p.sink.Publish(Update{
		Thread: st.Thread, Turn: st.Turn, Scope: st.Scope, Status: st.Status, StopReason: st.StopReason, Progress: true,
	})
}
