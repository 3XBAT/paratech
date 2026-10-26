package connect

import (
	"context"
	"errors"
	"time"

	connectrpc "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// Fixed client messages of AgentStep (errors.proto: never request text, model output or internals).
const (
	msgAgentInvalid   = "invalid agent step request"
	msgAgentTooLarge  = "the agent step request exceeds 256 KiB"
	msgUnknownCatalog = "unknown tool catalog version"
	msgBadSeal        = "the seal of echoed calls does not match"
	msgAgentDisabled  = "agent steps are not enabled on this Hub"
	msgAgentProvider  = "the LLM provider failed or answered without valid tool calls"
	msgTurnGone       = "the Hub no longer keeps the model turn of the echoed calls; start again from the student's text"
)

// AgentStepper is what the gateway handler needs from the agent step use case (ARCH-42).
type AgentStepper interface {
	Step(ctx context.Context, cmd agentstep.Command) (agentstep.Result, error)
}

// AgentStep runs one planner step for the authenticated caller. The request names no prompt, model or URL: the Hub
// picks them by profile (ADR-0023).
func (g *Gateway) AgentStep(ctx context.Context, req *connectrpc.Request[hubv1.AgentStepRequest]) (*connectrpc.Response[hubv1.AgentStepResponse], error) {
	owner, ok := APIKeyFrom(ctx)
	if !ok || owner == "" {
		return nil, rejectedError() // the auth middleware is missing; fail closed
	}
	if g.agent == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnimplemented, errors.New(msgAgentDisabled))
	}
	m := req.Msg
	cmd := agentstep.Command{
		Owner: owner, IdempotencyKey: m.GetIdempotencyKey(), ThreadID: m.GetThreadId(), Profile: profileOf(m.GetProfile()),
		CatalogVersion: m.GetCatalogVersion(), MaxOutputTokens: int64(m.GetMaxOutputTokens()),
		Base: agentstep.Base{Today: m.GetBase().GetToday(), TimeZone: m.GetBase().GetTimeZone()},
		Scope: agentstep.Scope{
			SubjectID: m.GetScope().GetSubjectId(), Screen: screenOf(m.GetScope().GetScreen()),
			LessonID: m.GetScope().GetLessonId(), AssignmentID: m.GetScope().GetAssignmentId(),
		},
	}
	for _, s := range m.GetBase().GetSubjects() {
		cmd.Base.Subjects = append(cmd.Base.Subjects, values.Subject{ID: s.GetSubjectId(), Name: s.GetName()})
	}
	for _, t := range m.GetTurns() {
		cmd.Turns = append(cmd.Turns, turnOf(t))
	}

	res, err := g.agent.Step(ctx, cmd)
	if err != nil {
		return nil, g.agentError(ctx, err)
	}
	quota, err := quotaToProto(res.Quota.Unit, res.Quota.Remaining, res.Quota.Reserved, res.Quota.Limit)
	if err != nil {
		g.log.ErrorContext(ctx, "agent step quota cannot be encoded", "err", err)
		return nil, connectrpc.NewError(connectrpc.CodeInternal, errors.New(msgInternal))
	}
	sealed := &hubv1.SealedCalls{Seal: res.Seal}
	for _, c := range res.Calls {
		sealed.Calls = append(sealed.Calls, &hubv1.ToolCall{CallId: c.ID, Tool: c.Tool, ArgumentsJson: c.Arguments})
	}
	return connectrpc.NewResponse(&hubv1.AgentStepResponse{
		Calls:         sealed,
		Usage:         &hubv1.Usage{InputTokens: res.InputTokens, OutputTokens: res.OutputTokens},
		Quota:         quota,
		ReservationId: res.ReservationID,
		Replayed:      res.Replayed,
	}), nil
}

// turnOf maps one history turn; a turn without a kind maps to the zero kind, which the domain rejects.
func turnOf(t *hubv1.AgentTurn) agentstep.Turn {
	switch v := t.GetTurn().(type) {
	case *hubv1.AgentTurn_StudentText:
		return agentstep.Turn{Kind: values.TurnStudentText, StudentText: v.StudentText}
	case *hubv1.AgentTurn_AgentReply:
		return agentstep.Turn{Kind: values.TurnAgentReply, AgentReply: v.AgentReply}
	case *hubv1.AgentTurn_Calls:
		out := agentstep.Turn{Kind: values.TurnCalls, Seal: v.Calls.GetSeal()}
		for _, c := range v.Calls.GetCalls() {
			out.Calls = append(out.Calls, agentstep.Call{ID: c.GetCallId(), Tool: c.GetTool(), Arguments: c.GetArgumentsJson()})
		}
		return out
	case *hubv1.AgentTurn_Results:
		out := agentstep.Turn{Kind: values.TurnResults}
		for _, r := range v.Results.GetResults() {
			out.Results = append(out.Results, agentstep.ToolResult{CallID: r.GetCallId(), JSON: r.GetResultJson(), IsError: r.GetIsError()})
		}
		return out
	default:
		return agentstep.Turn{}
	}
}

// profileOf maps the contract enum to the domain profile; UNSPECIFIED and unknown values map to "", which the
// domain rejects.
func profileOf(p hubv1.AgentProfile) string {
	if p == hubv1.AgentProfile_AGENT_PROFILE_PANEL {
		return string(values.ProfilePanel)
	}
	return ""
}

// screenOf maps the contract enum to the domain screen; an unknown value maps to a name the domain rejects.
func screenOf(s hubv1.AgentScreen) values.Screen {
	switch s {
	case hubv1.AgentScreen_AGENT_SCREEN_UNSPECIFIED:
		return values.ScreenUnspecified
	case hubv1.AgentScreen_AGENT_SCREEN_TODAY:
		return values.ScreenToday
	case hubv1.AgentScreen_AGENT_SCREEN_SUBJECTS:
		return values.ScreenSubjects
	case hubv1.AgentScreen_AGENT_SCREEN_ASSIGNMENTS:
		return values.ScreenAssignments
	case hubv1.AgentScreen_AGENT_SCREEN_SCHEDULE:
		return values.ScreenSchedule
	case hubv1.AgentScreen_AGENT_SCREEN_JOURNAL:
		return values.ScreenJournal
	case hubv1.AgentScreen_AGENT_SCREEN_SETTINGS:
		return values.ScreenSettings
	default:
		return "unknown"
	}
}

// agentError maps an agent step error to its Connect code and ErrorDetail (ADR-0005(c), errors.proto).
func (g *Gateway) agentError(ctx context.Context, err error) error {
	var (
		limited  *agentstep.RateLimitedError
		exceeded *agentstep.QuotaExceededError
		failed   *agentstep.ProviderFailedError
	)
	switch {
	case errors.As(err, &limited):
		return detailedError(connectrpc.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Message: msgRateLimited, Retryable: true,
			RetryAfter: durationpb.New(max(limited.RetryAfter, time.Millisecond)),
		})
	case errors.As(err, &exceeded):
		detail := &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: msgQuotaExceeded}
		q := exceeded.Quota
		if snapshot, err := quotaToProto(q.Unit, q.Remaining, q.Reserved, q.Limit); err == nil {
			detail.Quota = snapshot
		}
		return detailedError(connectrpc.CodeResourceExhausted, detail)
	case errors.As(err, &failed):
		detail := &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: msgAgentProvider, Retryable: failed.Transient}
		if failed.Transient && failed.RetryAfter > 0 {
			detail.RetryAfter = durationpb.New(failed.RetryAfter)
		}
		return detailedError(connectrpc.CodeUnavailable, detail)
	case errors.Is(err, agentstep.ErrIdempotencyConflict):
		return detailedError(connectrpc.CodeFailedPrecondition, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, Message: msgConflict,
		})
	case errors.Is(err, agentstep.ErrAttemptRunning):
		return connectrpc.NewError(connectrpc.CodeAborted, errors.New(msgRunning))
	case errors.Is(err, agentstep.ErrTurnGone):
		// Permanent and not charged; no new ErrorCode, so the contract stays as it is.
		return connectrpc.NewError(connectrpc.CodeFailedPrecondition, errors.New(msgTurnGone))
	case errors.Is(err, agentstep.ErrBadSeal):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgBadSeal))
	case errors.Is(err, agentstep.ErrUnknownCatalog):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgUnknownCatalog))
	case errors.Is(err, values.ErrAgentRequestTooLarge):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgAgentTooLarge))
	case errors.Is(err, agentstep.ErrInvalidRequest):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgAgentInvalid))
	case ctx.Err() != nil:
		return connectrpc.NewError(connectrpc.CodeCanceled, errors.New(msgCanceled))
	default:
		g.log.ErrorContext(ctx, "agent step failed", "err", err)
		return connectrpc.NewError(connectrpc.CodeInternal, errors.New(msgInternal))
	}
}
