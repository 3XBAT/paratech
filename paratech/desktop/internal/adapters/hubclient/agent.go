package hubclient

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	agent "paratech/desktop/internal/domain/agent/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const opAgentStep = "hubclient: agent step"

// maxOutputTokens bounds the output cap a step may ask for; the Hub clamps it far lower per profile.
const maxOutputTokens = 1 << 20

// ErrTurnGone means the Hub no longer keeps the model turn of the last echoed calls (failed_precondition without an
// ErrorDetail, ADR-0023): the request starts again from the student's text. It also matches agent.ErrTurnGone.
var ErrTurnGone = errors.New("hub no longer keeps the model turn")

// Step runs one planner step (GatewayService.AgentStep; the agentturn.AgentModel port) with the URL, key, limits and
// error classes of Complete. Only the contract fields are sent; the profile is PANEL and the catalog version the one
// this desktop implements, so the Hub picks the prompt and the model. The calls in the reply stay untrusted.
func (c *Client) Step(ctx context.Context, req agent.StepRequest) (agent.StepReply, error) {
	if err := ctx.Err(); err != nil {
		return agent.StepReply{}, fmt.Errorf("%s: %w", opAgentStep, err)
	}
	msg, err := agentRequestOf(req)
	if err != nil {
		return agent.StepReply{}, err
	}
	base, err := c.baseURL(ctx, opAgentStep)
	if err != nil {
		return agent.StepReply{}, err
	}
	key, err := c.apiKey(ctx, opAgentStep)
	if err != nil {
		return agent.StepReply{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(msg)
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.AgentStep(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return agent.StepReply{}, fmt.Errorf("%s: %w", opAgentStep, perr)
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return agent.StepReply{}, unavailable(opAgentStep, ErrTimeout)
		}
		if turnGone(err) {
			e := permanent(opAgentStep, ErrTurnGone)
			e.extra = []error{agent.ErrTurnGone}
			return agent.StepReply{}, e
		}
		return agent.StepReply{}, mapCallError(opAgentStep, err)
	}
	return agentReplyOf(resp.Msg)
}

// turnGone recognizes the Hub's own failed_precondition without a detail; the idempotency conflict carries one.
func turnGone(err error) bool {
	ce := new(connect.Error)
	return errors.As(err, &ce) && ce.Code() == connect.CodeFailedPrecondition && connect.IsWireError(err) &&
		errorDetail(ce) == nil
}

// agentRequestOf copies exactly the contract fields of the step.
func agentRequestOf(req agent.StepRequest) (*hubv1.AgentStepRequest, error) {
	if req.Key == "" || req.Thread == "" || req.MaxOutputTokens < 0 || req.MaxOutputTokens > maxOutputTokens ||
		len(req.History) == 0 {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	msg := &hubv1.AgentStepRequest{
		IdempotencyKey:  req.Key,
		ThreadId:        string(req.Thread),
		Profile:         hubv1.AgentProfile_AGENT_PROFILE_PANEL,
		CatalogVersion:  agent.CatalogVersion,
		Base:            &hubv1.AgentBase{Today: req.Base.Today().String(), TimeZone: req.Base.TimeZone()},
		MaxOutputTokens: uint32(req.MaxOutputTokens),
	}
	for _, s := range req.Base.Subjects() {
		msg.Base.Subjects = append(msg.Base.Subjects, &hubv1.AgentSubject{SubjectId: s.ID.ID(), Name: string(s.Name)})
	}
	screen, ok := screens[req.Scope.Screen()]
	if !ok {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	msg.Scope = &hubv1.AgentScope{
		SubjectId: req.Scope.Subject().ID(), Screen: screen, LessonId: req.Scope.Lesson().ID(),
		AssignmentId: req.Scope.Assignment().ID(),
	}
	for _, h := range req.History {
		t, err := turnOf(h)
		if err != nil {
			return nil, err
		}
		msg.Turns = append(msg.Turns, t)
	}
	return msg, nil
}

var screens = map[agent.Screen]hubv1.AgentScreen{
	"":                      hubv1.AgentScreen_AGENT_SCREEN_UNSPECIFIED,
	agent.ScreenToday:       hubv1.AgentScreen_AGENT_SCREEN_TODAY,
	agent.ScreenSubjects:    hubv1.AgentScreen_AGENT_SCREEN_SUBJECTS,
	agent.ScreenAssignments: hubv1.AgentScreen_AGENT_SCREEN_ASSIGNMENTS,
	agent.ScreenSchedule:    hubv1.AgentScreen_AGENT_SCREEN_SCHEDULE,
	agent.ScreenJournal:     hubv1.AgentScreen_AGENT_SCREEN_JOURNAL,
	agent.ScreenSettings:    hubv1.AgentScreen_AGENT_SCREEN_SETTINGS,
}

func turnOf(h agent.HistoryTurn) (*hubv1.AgentTurn, error) {
	if h.Validate() != nil {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	switch {
	case h.StudentText != "":
		return &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_StudentText{StudentText: h.StudentText}}, nil
	case h.AgentReply != "":
		return &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_AgentReply{AgentReply: h.AgentReply}}, nil
	case h.Calls != nil:
		sealed := &hubv1.SealedCalls{Seal: h.Calls.Seal}
		for _, c := range h.Calls.Calls {
			sealed.Calls = append(sealed.Calls, &hubv1.ToolCall{CallId: c.CallID, Tool: c.Tool, ArgumentsJson: c.ArgumentsJSON})
		}
		return &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Calls{Calls: sealed}}, nil
	default:
		results := &hubv1.ToolResults{}
		for _, r := range h.Results {
			results.Results = append(results.Results, &hubv1.ToolResult{CallId: r.CallID, ResultJson: r.ResultJSON, IsError: r.IsError})
		}
		return &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Results{Results: results}}, nil
	}
}

// agentReplyOf converts the answer; the calls stay untrusted (hard rule 3) and are checked by the agent domain.
func agentReplyOf(resp *hubv1.AgentStepResponse) (agent.StepReply, error) {
	calls := resp.GetCalls()
	if len(calls.GetCalls()) == 0 || len(calls.GetSeal()) == 0 {
		return agent.StepReply{}, permanent(opAgentStep, ErrBadResponse)
	}
	out := agent.StepReply{
		Calls:        agent.SealedCalls{Seal: calls.GetSeal()},
		InputTokens:  int(resp.GetUsage().GetInputTokens()),
		OutputTokens: int(resp.GetUsage().GetOutputTokens()),
		Replayed:     resp.GetReplayed(),
	}
	for _, c := range calls.GetCalls() {
		out.Calls.Calls = append(out.Calls.Calls, agent.RawCall{CallID: c.GetCallId(), Tool: c.GetTool(), ArgumentsJSON: c.GetArgumentsJson()})
	}
	if out.Validate() != nil {
		return agent.StepReply{}, permanent(opAgentStep, ErrBadResponse)
	}
	return out, nil
}
