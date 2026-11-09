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

// Step runs one planner step (the agentturn.AgentModel port) with the URL, key, limits and error classes of Complete.
// Only the contract fields are sent; the profile is PANEL and the catalog version the one this desktop implements, so
// the Hub picks the prompt and the model. The calls in the reply stay untrusted.
//
// It reads GatewayService.AgentStepStream (ADR-0026 addendum) and hands the reply text written so far, whole and
// cleaned (see replyDraft), to a non-nil draft, in order, from the calling goroutine; a Hub without it (unimplemented)
// is asked through AgentStep with the same request, so the same key, and so is every later step of this client. The
// reply is the stream's result, never the deltas; a stream that ends without a result is a lost answer (unavailable:
// the automatic retry of the case replays the key).
//
// A stream has no total cap of RequestTimeout: it asks for the Hub's heartbeat and ends as ErrTimeout when the Hub
// stays silent too long (see Config.StepIdleTimeout) or after Config.StepTimeout. The unary fallback is bounded by
// RequestTimeout.
func (c *Client) Step(ctx context.Context, req agent.StepRequest, draft func(soFar string)) (agent.StepReply, error) {
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

	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	if !c.noStepStream.Load() {
		resp, older, err := c.streamStep(ctx, gateway, msg, key, draft)
		if !older {
			if err != nil {
				return agent.StepReply{}, err
			}
			return agentReplyOf(resp)
		}
		c.noStepStream.Store(true)
	}

	// The unary step reports no progress, so it keeps the total cap of a call, on a context of its own.
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := unaryStep(callCtx, gateway, msg, key)
	if err != nil {
		return agent.StepReply{}, stepError(ctx, callCtx, err)
	}
	return agentReplyOf(resp)
}

// streamStep reads AgentStepStream within c.stepTO, cut when the Hub sends nothing for c.timeout before the first
// message or for c.stepIdle after one (watchStream). older reports an unimplemented stream while ctx lives (an older
// Hub), with the error unclassified; any other error is classified by stepError.
func (c *Client) streamStep(ctx context.Context, gateway hubv1connect.GatewayServiceClient, msg *hubv1.AgentStepRequest,
	key string, draft func(string),
) (res *hubv1.AgentStepResponse, older bool, err error) {
	callCtx, seen, release := watchStream(ctx, c.timeout, c.stepIdle, c.stepTO)
	defer release()

	res, err = readStepStream(callCtx, gateway, msg, key, draft, seen)
	switch {
	case err == nil:
		return res, false, nil
	case connect.CodeOf(err) == connect.CodeUnimplemented && ctx.Err() == nil:
		return nil, true, err
	}
	return nil, false, stepError(ctx, callCtx, err)
}

// readStepStream reads AgentStepStream with the Hub's heartbeat asked for, calling seen on every message: a heartbeat
// or an event this client does not know only does that. Errors are those of the generated client, or an *Error of a
// broken stream.
func readStepStream(ctx context.Context, gateway hubv1connect.GatewayServiceClient, msg *hubv1.AgentStepRequest,
	key string, draft func(string), seen func(),
) (*hubv1.AgentStepResponse, error) {
	sreq := connect.NewRequest(&hubv1.AgentStepStreamRequest{Request: msg, Heartbeat: true})
	sreq.Header().Set("Authorization", "Bearer "+key)
	stream, err := gateway.AgentStepStream(ctx, sreq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	var (
		text replyDraft
		res  *hubv1.AgentStepResponse
	)
	for stream.Receive() {
		seen()
		if res != nil {
			return nil, permanent(opAgentStep, ErrBadResponse) // the result is the last message
		}
		switch ev := stream.Msg().GetEvent().(type) {
		case *hubv1.AgentStepStreamResponse_Delta:
			if text.add(ev.Delta) && draft != nil {
				draft(text.String())
			}
		case *hubv1.AgentStepStreamResponse_Result:
			if ev.Result == nil {
				return nil, permanent(opAgentStep, ErrBadResponse)
			}
			res = ev.Result
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	if res == nil {
		// The Hub keeps the step it finished under the key: the automatic retry of a lost result replays it.
		return nil, unavailable(opAgentStep, ErrUnavailable)
	}
	return res, nil
}

// unaryStep is the step of a Hub without AgentStepStream.
func unaryStep(ctx context.Context, gateway hubv1connect.GatewayServiceClient, msg *hubv1.AgentStepRequest, key string,
) (*hubv1.AgentStepResponse, error) {
	creq := connect.NewRequest(msg)
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.AgentStep(ctx, creq)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// stepError classifies a failed step like Complete; the Hub's own failed_precondition is a model turn it forgot. The
// caller's ctx going away stays its error; a silent or overlong stream (the cause of callCtx) is a timeout.
func stepError(ctx, callCtx context.Context, err error) error {
	if perr := ctx.Err(); perr != nil {
		return fmt.Errorf("%s: %w", opAgentStep, perr)
	}
	if e := watchTimeout(callCtx, opAgentStep); e != nil {
		return e
	}
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return unavailable(opAgentStep, ErrTimeout)
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	if turnGone(err) {
		e := permanent(opAgentStep, ErrTurnGone)
		e.extra = []error{agent.ErrTurnGone}
		return e
	}
	return mapCallError(opAgentStep, err)
}

// turnGone recognizes the Hub's own failed_precondition without a detail; the idempotency conflict carries one.
func turnGone(err error) bool {
	ce := new(connect.Error)
	return errors.As(err, &ce) && ce.Code() == connect.CodeFailedPrecondition && connect.IsWireError(err) &&
		errorDetail(ce) == nil
}

// summaryCatalog is the first catalog whose Hub takes the history_summary turn (ADR-0028 «Into the window»).
const summaryCatalog = 9

// agentRequestOf copies exactly the contract fields of the step, for the catalog this desktop implements.
func agentRequestOf(req agent.StepRequest) (*hubv1.AgentStepRequest, error) {
	return agentRequestAt(req, agent.CatalogVersion)
}

// agentRequestAt is agentRequestOf on catalog version catalog: below summaryCatalog the summary turn is not sent (the
// window then opens with the student text that follows it).
func agentRequestAt(req agent.StepRequest, catalog uint32) (*hubv1.AgentStepRequest, error) {
	if req.Key == "" || req.Thread == "" || req.MaxOutputTokens < 0 || req.MaxOutputTokens > maxOutputTokens ||
		len(req.History) == 0 {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	msg := &hubv1.AgentStepRequest{
		IdempotencyKey:  req.Key,
		ThreadId:        string(req.Thread),
		Profile:         hubv1.AgentProfile_AGENT_PROFILE_PANEL,
		CatalogVersion:  catalog,
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
	var err error
	msg.Scope = &hubv1.AgentScope{
		SubjectId: req.Scope.Subject().ID(), Screen: screen, LessonId: req.Scope.Lesson().ID(),
		AssignmentId: req.Scope.Assignment().ID(),
	}
	if msg.Scope.Mentions, err = mentionsOf(req.Mentions); err != nil {
		return nil, err
	}
	for _, h := range req.History {
		if h.Summary != "" && catalog < summaryCatalog {
			continue
		}
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

var mentionKinds = map[agent.IDKind]hubv1.AgentMentionKind{
	agent.KindSubject:    hubv1.AgentMentionKind_AGENT_MENTION_KIND_SUBJECT,
	agent.KindLesson:     hubv1.AgentMentionKind_AGENT_MENTION_KIND_LESSON,
	agent.KindMaterial:   hubv1.AgentMentionKind_AGENT_MENTION_KIND_MATERIAL,
	agent.KindAssignment: hubv1.AgentMentionKind_AGENT_MENTION_KIND_ASSIGNMENT,
}

// mentionsOf copies kind, ID and the current flag of each mention, never more (the label stays local). An unknown
// kind, a repeated object or too many mentions is never sent: the kind is not guessed.
func mentionsOf(ms []agent.StepMention) ([]*hubv1.AgentMention, error) {
	if len(ms) > agent.MaxStepMentions {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	var out []*hubv1.AgentMention
	current, seen := 0, agent.RefSet{}
	for _, m := range ms {
		kind, ok := mentionKinds[m.Ref.Kind()]
		if !ok || m.Ref.ID() == "" || seen.Has(m.Ref) {
			return nil, permanent(opAgentStep, ErrInvalidRequest)
		}
		seen.Add(m.Ref)
		if m.Current {
			current++
		}
		out = append(out, &hubv1.AgentMention{Kind: kind, Id: m.Ref.ID(), Current: m.Current})
	}
	if current > agent.MaxMentions {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	return out, nil
}

func turnOf(h agent.HistoryTurn) (*hubv1.AgentTurn, error) {
	if h.Validate() != nil {
		return nil, permanent(opAgentStep, ErrInvalidRequest)
	}
	switch {
	case h.Summary != "":
		return &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_HistorySummary{HistorySummary: h.Summary}}, nil
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
