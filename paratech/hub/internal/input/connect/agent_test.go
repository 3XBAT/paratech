package connect_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/input/connect"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeStepper answers with res or err and remembers the commands.
type fakeStepper struct {
	mu   sync.Mutex
	res  agentstep.Result
	err  error
	cmds []agentstep.Command
}

func (f *fakeStepper) Step(_ context.Context, cmd agentstep.Command) (agentstep.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	return f.res, f.err
}

func agentServer(t *testing.T, stepper connect.AgentStepper, log *slog.Logger, opts ...connectrpc.ClientOption) hubv1connect.GatewayServiceClient {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	g, err := connect.NewGateway(&fakeCompleter{}, log, connect.WithAgentStep(stepper))
	if err != nil {
		t.Fatal(err)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Gateway: g})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...)
}

func agentRequest() *connectrpc.Request[hubv1.AgentStepRequest] {
	return withKey(connectrpc.NewRequest(&hubv1.AgentStepRequest{
		IdempotencyKey: "ag:th:1:2", ThreadId: "th", Profile: hubv1.AgentProfile_AGENT_PROFILE_PANEL, CatalogVersion: 1,
		Base: &hubv1.AgentBase{
			Subjects: []*hubv1.AgentSubject{{SubjectId: "s1", Name: "Матан"}}, Today: "2026-10-22", TimeZone: "Europe/Moscow",
		},
		Scope: &hubv1.AgentScope{SubjectId: "s1", Screen: hubv1.AgentScreen_AGENT_SCREEN_ASSIGNMENTS, LessonId: "l1", AssignmentId: "a1"},
		Turns: []*hubv1.AgentTurn{
			{Turn: &hubv1.AgentTurn_StudentText{StudentText: "что задали?"}},
			{Turn: &hubv1.AgentTurn_Calls{Calls: &hubv1.SealedCalls{
				Calls: []*hubv1.ToolCall{{CallId: "toolu_1", Tool: "list_subjects", ArgumentsJson: "{}"}}, Seal: []byte{1, 2},
			}}},
			{Turn: &hubv1.AgentTurn_Results{Results: &hubv1.ToolResults{
				Results: []*hubv1.ToolResult{{CallId: "toolu_1", ResultJson: "[]", IsError: true}},
			}}},
			{},
		},
		MaxOutputTokens: 300,
	}), anyKey)
}

func TestAgentStepMapsTheRequestAndTheResult(t *testing.T) {
	svc := &fakeStepper{res: agentstep.Result{
		Calls: []agentstep.Call{{ID: "toolu_2", Tool: "reply", Arguments: `{"text":"Готово."}`}}, Seal: []byte{1, 9, 9},
		InputTokens: 40, OutputTokens: 7, ReservationID: strings.Repeat("cd", 16), Replayed: true,
		Quota: agentstep.Snapshot{Unit: "tokens", Remaining: 50, Limit: 100},
	}}
	for codec, opts := range codecs {
		client := agentServer(t, svc, nil, opts...)
		resp, err := client.AgentStep(t.Context(), agentRequest())
		if err != nil {
			t.Fatalf("%s: %v", codec, err)
		}
		m := resp.Msg
		c := m.GetCalls().GetCalls()
		if len(c) != 1 || c[0].GetCallId() != "toolu_2" || c[0].GetTool() != "reply" || c[0].GetArgumentsJson() != `{"text":"Готово."}` ||
			!bytes.Equal(m.GetCalls().GetSeal(), []byte{1, 9, 9}) || m.GetUsage().GetInputTokens() != 40 || !m.GetReplayed() ||
			m.GetReservationId() != strings.Repeat("cd", 16) || m.GetQuota().GetRemaining().GetValue() != 50 {
			t.Fatalf("%s: response = %v", codec, m)
		}
	}
	want := agentstep.Command{
		Owner: "00000000000000aa", IdempotencyKey: "ag:th:1:2", ThreadID: "th", Profile: "panel", CatalogVersion: 1, MaxOutputTokens: 300,
		Base:  agentstep.Base{Subjects: []values.Subject{{ID: "s1", Name: "Матан"}}, Today: "2026-10-22", TimeZone: "Europe/Moscow"},
		Scope: agentstep.Scope{SubjectID: "s1", Screen: values.ScreenAssignments, LessonID: "l1", AssignmentID: "a1"},
		Turns: []agentstep.Turn{
			{Kind: values.TurnStudentText, StudentText: "что задали?"},
			{Kind: values.TurnCalls, Calls: []agentstep.Call{{ID: "toolu_1", Tool: "list_subjects", Arguments: "{}"}}, Seal: []byte{1, 2}},
			{Kind: values.TurnResults, Results: []agentstep.ToolResult{{CallID: "toolu_1", JSON: "[]", IsError: true}}},
			{}, // a turn without a kind: the domain rejects it
		},
	}
	if !reflect.DeepEqual(svc.cmds[0], want) {
		t.Fatalf("command = %+v\nwant      %+v", svc.cmds[0], want)
	}
}

// TestAgentStepMapsTheAgentReply: agent_reply maps to its own turn kind, kept apart from the student's text.
func TestAgentStepMapsTheAgentReply(t *testing.T) {
	for codec, opts := range codecs {
		svc := &fakeStepper{err: agentstep.ErrInvalidRequest}
		client := agentServer(t, svc, nil, opts...)
		req := agentRequest()
		req.Msg.Turns = []*hubv1.AgentTurn{
			{Turn: &hubv1.AgentTurn_StudentText{StudentText: "что такое горутина?"}},
			{Turn: &hubv1.AgentTurn_AgentReply{AgentReply: "reply: Лёгкий поток."}},
			{Turn: &hubv1.AgentTurn_StudentText{StudentText: "кто такой Рыбаков?"}},
		}
		_, _ = client.AgentStep(t.Context(), req)
		want := []agentstep.Turn{
			{Kind: values.TurnStudentText, StudentText: "что такое горутина?"},
			{Kind: values.TurnAgentReply, AgentReply: "reply: Лёгкий поток."},
			{Kind: values.TurnStudentText, StudentText: "кто такой Рыбаков?"},
		}
		if got := svc.cmds[0].Turns; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: turns = %+v, want %+v", codec, got, want)
		}
	}
}

func TestAgentStepMapsEnums(t *testing.T) {
	svc := &fakeStepper{err: agentstep.ErrInvalidRequest}
	client := agentServer(t, svc, nil)
	screens := map[hubv1.AgentScreen]values.Screen{
		hubv1.AgentScreen_AGENT_SCREEN_UNSPECIFIED: "", hubv1.AgentScreen_AGENT_SCREEN_TODAY: "today",
		hubv1.AgentScreen_AGENT_SCREEN_SUBJECTS: "subjects", hubv1.AgentScreen_AGENT_SCREEN_ASSIGNMENTS: "assignments",
		hubv1.AgentScreen_AGENT_SCREEN_SCHEDULE: "schedule", hubv1.AgentScreen_AGENT_SCREEN_JOURNAL: "journal",
		hubv1.AgentScreen_AGENT_SCREEN_SETTINGS: "settings", hubv1.AgentScreen(42): "unknown",
	}
	for screen, want := range screens {
		req := agentRequest()
		req.Msg.Scope.Screen = screen
		_, _ = client.AgentStep(t.Context(), req)
		if got := svc.cmds[len(svc.cmds)-1].Scope.Screen; got != want {
			t.Errorf("%v: screen %q, want %q", screen, got, want)
		}
	}
	for profile, want := range map[hubv1.AgentProfile]string{
		hubv1.AgentProfile_AGENT_PROFILE_PANEL: "panel", hubv1.AgentProfile_AGENT_PROFILE_UNSPECIFIED: "", hubv1.AgentProfile(7): "",
	} {
		req := agentRequest()
		req.Msg.Profile = profile
		_, _ = client.AgentStep(t.Context(), req)
		if got := svc.cmds[len(svc.cmds)-1].Profile; got != want {
			t.Errorf("%v: profile %q, want %q", profile, got, want)
		}
	}
}

func TestAgentStepErrorMapping(t *testing.T) {
	transient := &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded", Status: 529}
	tests := []struct {
		name      string
		err       error
		code      connectrpc.Code
		detail    hubv1.ErrorCode
		retryable bool
		message   string
	}{
		{name: "rate limited", err: &agentstep.RateLimitedError{RetryAfter: time.Second}, code: connectrpc.CodeResourceExhausted,
			detail: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, retryable: true},
		{name: "quota", err: &agentstep.QuotaExceededError{Quota: agentstep.Snapshot{Unit: "tokens", Remaining: 1, Limit: 2}},
			code: connectrpc.CodeResourceExhausted, detail: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED},
		{name: "transient provider", err: &agentstep.ProviderFailedError{Transient: true, Cause: transient}, code: connectrpc.CodeUnavailable,
			detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, retryable: true},
		{name: "invalid tool calls", err: &agentstep.ProviderFailedError{Cause: errors.New("invalid tool calls")}, code: connectrpc.CodeUnavailable,
			detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR},
		{name: "conflict", err: agentstep.ErrIdempotencyConflict, code: connectrpc.CodeFailedPrecondition,
			detail: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT},
		{name: "running", err: agentstep.ErrAttemptRunning, code: connectrpc.CodeAborted},
		{name: "turn gone", err: fmt.Errorf("agentstep: kept turn: %w", agentstep.ErrTurnGone), code: connectrpc.CodeFailedPrecondition,
			message: "the Hub no longer keeps the model turn of the echoed calls; start again from the student's text"},
		{name: "bad seal", err: fmt.Errorf("x: %w", agentstep.ErrBadSeal), code: connectrpc.CodeInvalidArgument,
			message: "the seal of echoed calls does not match"},
		{name: "unknown catalog", err: agentstep.ErrUnknownCatalog, code: connectrpc.CodeInvalidArgument, message: "unknown tool catalog version"},
		{name: "too large", err: fmt.Errorf("%w: %w", agentstep.ErrInvalidRequest, values.ErrAgentRequestTooLarge),
			code: connectrpc.CodeInvalidArgument, message: "the agent step request exceeds 256 KiB"},
		{name: "invalid", err: fmt.Errorf("%w: %w", agentstep.ErrInvalidRequest, values.ErrInvalidAgentRequest),
			code: connectrpc.CodeInvalidArgument, message: "invalid agent step request"},
		{name: "hub failure", err: errors.New("db down at db.internal:5432"), code: connectrpc.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged bytes.Buffer
			client := agentServer(t, &fakeStepper{err: tt.err}, slog.New(slog.NewTextHandler(&logged, nil)))
			_, err := client.AgentStep(t.Context(), agentRequest())
			code, d, msg := detailOf(t, err)
			if code != tt.code {
				t.Fatalf("code = %v, want %v", code, tt.code)
			}
			if tt.detail == hubv1.ErrorCode_ERROR_CODE_UNSPECIFIED && d != nil {
				t.Fatalf("unexpected detail %v", d)
			}
			if tt.detail != hubv1.ErrorCode_ERROR_CODE_UNSPECIFIED && (d == nil || d.GetCode() != tt.detail || d.GetRetryable() != tt.retryable) {
				t.Fatalf("detail = %v", d)
			}
			if tt.message != "" && msg != tt.message {
				t.Fatalf("message = %q, want %q", msg, tt.message)
			}
			for _, leak := range []string{"overloaded", "529", "db.internal", "invalid tool calls"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("answer %q leaks %q", msg, leak)
				}
			}
		})
	}
}

func TestAgentStepNeedsAKeyAndAStepper(t *testing.T) {
	client := agentServer(t, &fakeStepper{}, nil)
	req := agentRequest()
	req.Header().Del("Authorization")
	if _, err := client.AgentStep(t.Context(), req); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("without a key: %v", err)
	}
	disabled := gatewayServer(t, &fakeCompleter{}, nil)
	c := hubv1connect.NewGatewayServiceClient(disabled.Client(), disabled.URL)
	if _, err := c.AgentStep(t.Context(), agentRequest()); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("without a stepper: %v", err)
	}
}
