package agentstep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in errors and logs.
const (
	opStep    = "step"
	opStart   = "start"
	opSettle  = "settle"
	opRelease = "release"
	opReplay  = "replay"
	opTurn    = "kept turn"
	opPurge   = "purge kept turns"
)

// Outcomes in the log line of a step.
const (
	outcomeCompleted   = "completed"
	outcomeReplayed    = "replayed"
	outcomeInvalid     = "invalid_request"
	outcomeBadSeal     = "bad_seal"
	outcomeRateLimited = "rate_limited"
	outcomeQuota       = "quota_exceeded"
	outcomeConflict    = "idempotency_conflict"
	outcomeRunning     = "attempt_running"
	outcomeProvider    = "provider_error"
	outcomeTurnGone    = "turn_gone"
	outcomeCanceled    = "canceled"
	outcomeFailed      = "failed"
)

// ledgerTimeout bounds each Start, Settle or Release, which run on a context detached from the client (as in the
// gateway, ADR-0010).
const ledgerTimeout = 5 * time.Second

// profile is a validated Config.Profiles entry.
type profile struct {
	alias     string
	versions  map[uint32]Version
	minOutput int64
}

// Service runs the agent step scenario (ADR-0023).
type Service struct {
	accounts    Accounts
	turns       AssistantTurns
	provider    Provider
	limiter     RateLimiter
	clock       Clock
	random      Random
	models      *modelallowlist.List
	sealer      *seal.Sealer
	log         *slog.Logger
	policy      account.Policy
	callTimeout time.Duration
	profiles    map[values.Profile]profile
}

// New checks the dependencies and the configuration: every profile is known, its alias is in the allowlist and it
// has at least one version, each with a server prompt and a catalog of that version.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Accounts == nil || d.Turns == nil || d.Provider == nil || d.Limiter == nil || d.Clock == nil || d.Random == nil ||
		d.Models == nil || d.Sealer == nil || d.Log == nil {
		return nil, fmt.Errorf("%w: every dependency is required", ErrInvalidConfig)
	}
	p, err := account.NewPolicy(cfg.Unit, cfg.Limit, cfg.ReservationTTL, cfg.KeyRetention)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if cfg.CallTimeout <= 0 || cfg.CallTimeout+2*ledgerTimeout > cfg.ReservationTTL {
		return nil, fmt.Errorf("%w: the call timeout plus %s must be above 0 and within the reservation TTL", ErrInvalidConfig, 2*ledgerTimeout)
	}
	profiles, err := buildProfiles(d.Models, cfg.Profiles)
	if err != nil {
		return nil, err
	}
	return &Service{
		accounts: d.Accounts, turns: d.Turns, provider: d.Provider, limiter: d.Limiter, clock: d.Clock, random: d.Random, models: d.Models,
		sealer: d.Sealer, log: d.Log, policy: p, callTimeout: cfg.CallTimeout, profiles: profiles,
	}, nil
}

func buildProfiles(models *modelallowlist.List, in []Profile) (map[values.Profile]profile, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: no profile", ErrInvalidConfig)
	}
	out := make(map[values.Profile]profile, len(in))
	for _, p := range in {
		name, err := values.ParseProfile(string(p.Name))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%w: profile %s is configured twice", ErrInvalidConfig, name)
		}
		if _, _, err := models.Resolve(p.Alias, 0); err != nil {
			return nil, fmt.Errorf("%w: profile %s: alias %q: %w", ErrInvalidConfig, name, p.Alias, err)
		}
		versions := make(map[uint32]Version, len(p.Versions))
		for _, v := range p.Versions {
			if v.Catalog == nil || v.System == "" {
				return nil, fmt.Errorf("%w: profile %s: a version needs a catalog and a system prompt", ErrInvalidConfig, name)
			}
			if _, dup := versions[v.Catalog.Version()]; dup {
				return nil, fmt.Errorf("%w: profile %s: catalog version %d is configured twice", ErrInvalidConfig, name, v.Catalog.Version())
			}
			versions[v.Catalog.Version()] = v
		}
		if len(versions) == 0 {
			return nil, fmt.Errorf("%w: profile %s has no version", ErrInvalidConfig, name)
		}
		minOutput := p.MinOutput
		if minOutput == 0 {
			minOutput = DefaultMinOutput
		}
		if minOutput < 0 {
			return nil, fmt.Errorf("%w: profile %s: the output floor must not be negative", ErrInvalidConfig, name)
		}
		out[name] = profile{alias: p.Alias, versions: versions, minOutput: minOutput}
	}
	return out, nil
}

// step is a validated Step command.
type step struct {
	owner   shared.APIKeyID
	binding seal.Binding
	version Version
	reserve account.ReserveRequest
	call    values.AgentCall
	// echoKey names the call set of the last echo, whose kept model turn the provider needs back; "" without an echo.
	echoKey string
}

// Step runs one planner step for cmd.Owner. Errors: *RateLimitedError, ErrInvalidRequest (ErrUnknownCatalog,
// ErrBadSeal), *QuotaExceededError, ErrIdempotencyConflict, ErrAttemptRunning, ErrTurnGone, *ProviderFailedError, the client's
// context error; anything else is a Hub failure. A provider call that has started is finished, settled and stored even
// if the client goes away.
func (s *Service) Step(ctx context.Context, cmd Command) (Result, error) {
	// One log line per step: outcome, profile, catalog version, alias, reservation, usage, stop reason of a provider
	// answer, number of calls, duration; never texts, arguments, results or prompts.
	begin := s.clock.Now()
	logStep := func(outcome string, attrs ...any) {
		attrs = append([]any{"outcome", outcome, "duration", s.clock.Now().Sub(begin)}, attrs...)
		s.log.InfoContext(ctx, "agent step", attrs...)
	}
	if err := cmd.Owner.Validate(); err != nil {
		logStep(outcomeInvalid)
		return Result{}, fmt.Errorf("agentstep: %s: %w: %w", opStep, ErrInvalidRequest, err)
	}
	// Before anything else, so a limited call costs no validation of a large body and writes no ledger row.
	if wait, ok := s.limiter.Allow(cmd.Owner, begin); !ok {
		logStep(outcomeRateLimited, "retry_after", wait)
		return Result{}, &RateLimitedError{RetryAfter: wait}
	}
	st, err := s.validate(cmd)
	if err != nil {
		outcome := outcomeInvalid
		if errors.Is(err, ErrBadSeal) {
			outcome = outcomeBadSeal
		}
		logStep(outcome)
		return Result{}, fmt.Errorf("agentstep: %s: %w", opStep, err)
	}
	attrs := []any{"profile", string(st.binding.Profile), "catalog_version", st.binding.CatalogVersion,
		"alias", string(st.reserve.Fingerprint().Alias)}

	rec, stored, quota, err := s.start(ctx, st)
	if err != nil {
		var exceeded *QuotaExceededError
		switch {
		case errors.As(err, &exceeded):
			logStep(outcomeQuota, attrs...)
		case errors.Is(err, ErrIdempotencyConflict):
			logStep(outcomeConflict, attrs...)
		case errors.Is(err, ErrAttemptRunning):
			logStep(outcomeRunning, attrs...)
		case ctx.Err() != nil:
			logStep(outcomeCanceled, attrs...)
		default:
			s.log.ErrorContext(ctx, "agent step start failed", "err", err)
			logStep(outcomeFailed, attrs...)
		}
		return Result{}, err
	}
	attrs = append(attrs, "reservation", rec.ID.String())
	if stored.Stored && stored.Output == values.FailedStepOutput {
		// Charged once already: the same request gets the same failure, never a second paid call.
		logStep(outcomeProvider, append(attrs, "replayed", true, "input_tokens", stored.InputTokens, "output_tokens", stored.OutputTokens)...)
		return Result{}, &ProviderFailedError{Cause: &values.ProviderError{Kind: values.KindPermanent, Reason: "no usable tool calls"}}
	}
	if stored.Stored {
		calls, err := values.DecodeCalls(stored.Output)
		if err != nil {
			s.log.ErrorContext(ctx, "agent step stored result unreadable", "reservation", rec.ID.String(), "err", err)
			logStep(outcomeFailed, attrs...)
			return Result{}, fmt.Errorf("agentstep: %s: %w", opReplay, err)
		}
		logStep(outcomeReplayed, append(attrs, "input_tokens", stored.InputTokens, "output_tokens", stored.OutputTokens, "calls", len(calls))...)
		return s.result(st, calls, stored.InputTokens, stored.OutputTokens, quota, rec.ID, true), nil
	}
	// The client left before the provider was called: nothing was used, the hold goes back whole.
	if err := ctx.Err(); err != nil {
		s.release(ctx, st, rec.ID, account.ReasonExplicit)
		logStep(outcomeCanceled, attrs...)
		return Result{}, fmt.Errorf("agentstep: %s: %w", opStep, err)
	}
	// Looked up only now, so a replay never depends on the kept turn. Without it the provider would refuse the
	// continuation (or lose the model's reasoning), so the step fails instead and nothing is charged.
	call, err := s.withKeptTurn(ctx, st)
	if err != nil {
		s.release(ctx, st, rec.ID, account.ReasonFailed)
		if errors.Is(err, ErrTurnGone) {
			logStep(outcomeTurnGone, attrs...)
		} else {
			s.log.ErrorContext(ctx, "agent step kept turn unreadable", "reservation", rec.ID.String(), "err", err)
			logStep(outcomeFailed, attrs...)
		}
		return Result{}, err
	}
	timeout := min(s.callTimeout, rec.ExpiresAt.Sub(s.clock.Now())-ledgerTimeout)
	if timeout <= 0 {
		s.release(ctx, st, rec.ID, account.ReasonFailed)
		logStep(outcomeFailed, attrs...)
		return Result{}, fmt.Errorf("agentstep: %s: no time left for the provider call before the reservation expires", opStep)
	}
	// Detached from the client (unary rule, gateway.proto): a started call finishes and is paid for and stored.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	answer, err := s.provider.Step(callCtx, call)
	cancel()
	if err == nil {
		// Says why a step failed, for example an answer cut at the output cap.
		attrs = append(attrs, "stop_reason", answer.Stop().String())
		if answer.Refused() {
			err = answer.Failure()
		}
	}
	if err != nil {
		// No answer, or a refusal: released, not charged, like in Complete (accepted gap, ADR-0010).
		s.release(ctx, st, rec.ID, account.ReasonFailed)
		failure := providerFailure(err)
		logStep(outcomeProvider, append(attrs, "transient", failure.Transient, "err", err)...)
		return Result{}, failure
	}

	usage := answer.Usage()
	calls := answer.Calls()
	unusable := answer.Failure()
	textReply := false
	if unusable == nil {
		// A reply text over its limit (catalog 7) is cut at a sentence rather than failed and charged (TASK-223); the
		// echo carries the cut calls, so the kept turn is found under them.
		var cut bool
		if calls, cut = st.version.Catalog.FitReturned(calls); cut {
			attrs = append(attrs, "reply_cut", true)
		}
		// The model's calls are untrusted: a call outside the catalog schema is the provider's failure (ADR-0023).
		if cerr := st.version.Catalog.CheckReturned(calls); cerr != nil {
			unusable = &values.ProviderError{Kind: values.KindPermanent, Reason: "invalid tool calls"}
		}
	} else if text, ok := answer.PlainText(); ok {
		// The model ended its turn with text only (tool choice auto, no forced tool use with thinking): that text is
		// shown as a reply card if the reply schema takes it, and is never anything else (TASK-200).
		reply, rerr := st.version.Catalog.TextReply(textReplyID(rec.ID), text)
		if rerr == nil {
			calls, unusable, textReply = []values.ToolCall{reply}, nil, true
		}
		attrs = append(attrs, "text_reply", rerr == nil)
	}
	if unusable != nil {
		// The provider billed this answer (output cap, end of turn, invalid calls): charged like Complete charges a
		// truncated answer, and stored as a failure so a retry with the key repeats it instead of paying again.
		if _, err := s.settle(ctx, st, rec.ID, values.FailedStepOutput, usage); err != nil {
			s.log.ErrorContext(ctx, "agent step settlement failed", "reservation", rec.ID.String(), "err", err)
			logStep(outcomeFailed, append(attrs, "input_tokens", usage.Input(), "output_tokens", usage.Output())...)
			return Result{}, err
		}
		failure := providerFailure(unusable)
		logStep(outcomeProvider, append(attrs, "charged", true, "input_tokens", usage.Input(), "output_tokens", usage.Output(),
			"reason", failureReason(unusable), "err", unusable)...)
		return Result{}, failure
	}

	// Kept before the settlement, so a replayed answer can always be continued. A failure is only logged: the answer is
	// paid for and returned, and only a continuation of it fails (ErrTurnGone). A text reply keeps nothing: its model
	// turn holds no call to send back, so a continuation of it is ErrTurnGone, never a made-up turn.
	if !textReply {
		if err := s.keep(ctx, st, calls, answer.Assistant()); err != nil {
			s.log.ErrorContext(ctx, "agent step turn not kept", "reservation", rec.ID.String(), "err", err)
		}
	}
	quota, err = s.settle(ctx, st, rec.ID, values.EncodeCalls(calls), usage)
	if err != nil {
		s.log.ErrorContext(ctx, "agent step settlement failed", "reservation", rec.ID.String(), "err", err)
		logStep(outcomeFailed, append(attrs, "input_tokens", usage.Input(), "output_tokens", usage.Output())...)
		return Result{}, err
	}
	logStep(outcomeCompleted, append(attrs, "input_tokens", usage.Input(), "output_tokens", usage.Output(), "calls", len(calls))...)
	return s.result(st, calls, usage.Input(), usage.Output(), quota, rec.ID, false), nil
}

// validate builds the domain values of cmd in the order of ADR-0023: limits and format, catalog version, seals of the
// echoed calls, then the provider call and the reservation of the most it may cost.
func (s *Service) validate(cmd Command) (step, error) {
	req, err := request(cmd)
	if err != nil {
		return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	p, ok := s.profiles[req.Profile()]
	if !ok {
		return step{}, fmt.Errorf("%w: the profile is not served", ErrInvalidRequest)
	}
	version, ok := p.versions[req.CatalogVersion()]
	if !ok {
		return step{}, ErrUnknownCatalog
	}
	binding := seal.Binding{Owner: cmd.Owner, ThreadID: req.ThreadID(), Profile: req.Profile(), CatalogVersion: req.CatalogVersion()}
	for _, t := range req.Turns() {
		if t.Kind() != values.TurnCalls {
			continue
		}
		if err := version.Catalog.CheckEcho(t.Calls()); err != nil {
			return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		if err := s.sealer.Verify(binding, t.Calls(), t.Seal()); err != nil {
			return step{}, ErrBadSeal
		}
	}

	// Raised to the floor before the alias cap, so the reservation covers the cap actually sent.
	requested := req.MaxOutput()
	if requested > 0 && requested < p.minOutput {
		requested = p.minOutput
	}
	model, maxOutput, err := s.models.Resolve(p.alias, requested)
	if err != nil {
		return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	call, err := values.NewAgentCall(model, version.System, version.Catalog.Specs(), req, maxOutput)
	if err != nil {
		return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	amount, err := requestlimits.AgentReservation(call)
	if err != nil {
		return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	reserve, err := account.NewReserveRequest(req.Key(), string(values.OperationAgentStep), p.alias, string(s.policy.Unit()), amount)
	if err == nil {
		reserve, err = reserve.WithRequestHash(req.Fingerprint())
	}
	if err != nil {
		return step{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	st := step{owner: cmd.Owner, binding: binding, version: version, reserve: reserve, call: call}
	turns := req.Turns()
	if i := values.LastCalls(turns); i >= 0 {
		st.echoKey = callSetKey(binding, turns[i].Calls())
	}
	return st, nil
}

func callSetKey(b seal.Binding, calls []values.ToolCall) string {
	return values.CallSetKey(b.ThreadID, b.Profile, b.CatalogVersion, calls)
}

// request builds the domain request of cmd; every part is checked by its constructor.
func request(cmd Command) (values.AgentRequest, error) {
	profile, err := values.ParseProfile(cmd.Profile)
	if err != nil {
		return values.AgentRequest{}, err
	}
	base, err := values.NewAgentBase(cmd.Base.Subjects, cmd.Base.Today, cmd.Base.TimeZone)
	if err != nil {
		return values.AgentRequest{}, err
	}
	scope, err := values.NewAgentScope(cmd.Scope.SubjectID, cmd.Scope.Screen, cmd.Scope.LessonID, cmd.Scope.AssignmentID)
	if err != nil {
		return values.AgentRequest{}, err
	}
	if len(cmd.Scope.Mentions) > values.MaxAgentMentions {
		return values.AgentRequest{}, fmt.Errorf("%w: more than %d mentions", values.ErrInvalidAgentRequest, values.MaxAgentMentions)
	}
	mentions := make([]values.Mention, 0, len(cmd.Scope.Mentions))
	for _, m := range cmd.Scope.Mentions {
		mention, err := values.NewMention(m.Kind, m.ID, m.Current)
		if err != nil {
			return values.AgentRequest{}, err
		}
		mentions = append(mentions, mention)
	}
	if scope, err = scope.WithMentions(mentions); err != nil {
		return values.AgentRequest{}, err
	}
	if len(cmd.Turns) > values.MaxAgentTurns {
		return values.AgentRequest{}, fmt.Errorf("%w: a request holds 1-%d turns", values.ErrInvalidAgentRequest, values.MaxAgentTurns)
	}
	turns := make([]values.Turn, 0, len(cmd.Turns))
	for _, t := range cmd.Turns {
		turn, err := turnOf(t)
		if err != nil {
			return values.AgentRequest{}, err
		}
		turns = append(turns, turn)
	}
	return values.NewAgentRequest(cmd.IdempotencyKey, cmd.ThreadID, profile, cmd.CatalogVersion, base, scope, turns, cmd.MaxOutputTokens)
}

func turnOf(t Turn) (values.Turn, error) {
	switch t.Kind {
	case values.TurnStudentText:
		return values.StudentTurn(t.StudentText)
	case values.TurnAgentReply:
		return values.AgentReplyTurn(t.AgentReply)
	case values.TurnHistorySummary:
		return values.HistorySummaryTurn(t.Summary)
	case values.TurnCalls:
		if len(t.Calls) > values.MaxAgentCalls {
			return values.Turn{}, fmt.Errorf("%w: an echo holds 1-%d calls", values.ErrInvalidAgentRequest, values.MaxAgentCalls)
		}
		calls := make([]values.ToolCall, 0, len(t.Calls))
		for _, c := range t.Calls {
			call, err := values.NewToolCall(c.ID, c.Tool, c.Arguments)
			if err != nil {
				return values.Turn{}, err
			}
			calls = append(calls, call)
		}
		return values.CallsTurn(calls, t.Seal)
	case values.TurnResults:
		if len(t.Results) > values.MaxAgentCalls {
			return values.Turn{}, fmt.Errorf("%w: a results turn holds 1-%d results", values.ErrInvalidAgentRequest, values.MaxAgentCalls)
		}
		results := make([]values.ToolResult, 0, len(t.Results))
		for _, r := range t.Results {
			res, err := values.NewToolResult(r.CallID, r.JSON, r.IsError)
			if err != nil {
				return values.Turn{}, err
			}
			results = append(results, res)
		}
		return values.ResultsTurn(results)
	default:
		return values.Turn{}, fmt.Errorf("%w: unknown turn kind", values.ErrInvalidAgentRequest)
	}
}

// result seals calls for the caller and thread of st with the current key; a replay is sealed afresh, so it stays
// valid after a key rotation.
func (s *Service) result(st step, calls []values.ToolCall, in, out int64, quota Snapshot, id shared.ReservationID, replayed bool) Result {
	dto := make([]Call, 0, len(calls))
	for _, c := range calls {
		dto = append(dto, Call{ID: c.CallID(), Tool: c.Tool(), Arguments: c.Arguments()})
	}
	return Result{
		Calls: dto, Seal: s.sealer.Seal(st.binding, calls), InputTokens: in, OutputTokens: out, Quota: quota,
		ReservationID: id.String(), Replayed: replayed,
	}
}

// withKeptTurn is the provider call of st with the kept model turn of its last echo attached. A turn that is not kept
// (any more) is ErrTurnGone; a zero turn means the provider gave none and the echo goes out as it is.
func (s *Service) withKeptTurn(ctx context.Context, st step) (values.AgentCall, error) {
	if st.echoKey == "" {
		return st.call, nil
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	turn, ok, err := s.turns.Kept(readCtx, st.owner, st.echoKey, s.policy.ResultCutoff(s.clock.Now()))
	switch {
	case err != nil:
		return values.AgentCall{}, fmt.Errorf("agentstep: %s: %w", opTurn, err)
	case !ok:
		return values.AgentCall{}, fmt.Errorf("agentstep: %s: %w", opTurn, ErrTurnGone)
	case turn.IsZero():
		return st.call, nil
	}
	call, err := st.call.WithAssistant(turn)
	if err != nil {
		return values.AgentCall{}, fmt.Errorf("agentstep: %s: %w", opTurn, err)
	}
	return call, nil
}

// keep stores the model turn of the returned calls under their call set for the next step.
func (s *Service) keep(ctx context.Context, st step, calls []values.ToolCall, turn values.AssistantTurn) error {
	keepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	if err := s.turns.Keep(keepCtx, st.owner, callSetKey(st.binding, calls), turn, s.clock.Now()); err != nil {
		return fmt.Errorf("agentstep: %s: %w", opTurn, err)
	}
	return nil
}

// PurgeResults deletes the kept model turns whose result window has ended (ADR-0005(d), ADR-0023) and returns how
// many; a turn lives no longer than a stored result.
func (s *Service) PurgeResults(ctx context.Context) (int64, error) {
	n, err := s.turns.PurgeTurns(ctx, s.policy.ResultCutoff(s.clock.Now()))
	if err != nil {
		return 0, fmt.Errorf("agentstep: %s: %w", opPurge, err)
	}
	return n, nil
}

// start opens a new attempt or finds the stored result of the latest one, in one transaction on a context detached
// from the client with its own bound (see the gateway use case for why).
func (s *Service) start(ctx context.Context, st step) (account.ReservationRecord, account.Result, Snapshot, error) {
	id, err := s.newID()
	if err != nil {
		return account.ReservationRecord{}, account.Result{}, Snapshot{}, fmt.Errorf("agentstep: %s: %w", opStart, err)
	}
	now := s.clock.Now()
	var (
		rec    account.ReservationRecord
		stored account.Result
		quota  Snapshot
	)
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	err = s.accounts.Modify(startCtx, s.scope(st, now, ""), func(a *account.Account) error {
		r, res, err := a.Start(st.reserve, id, now)
		if errors.Is(err, account.ErrQuotaExceeded) {
			return &QuotaExceededError{Quota: snapshotOf(a.Snapshot(now))}
		}
		if err != nil {
			return err
		}
		rec, stored, quota = r, res, snapshotOf(a.Snapshot(now))
		return nil
	})
	switch {
	case err == nil:
		return rec, stored, quota, nil
	case errors.Is(err, account.ErrIdempotencyConflict):
		return rec, stored, quota, fmt.Errorf("agentstep: %s: %w", opStart, ErrIdempotencyConflict)
	case errors.Is(err, account.ErrAttemptRunning):
		return rec, stored, quota, fmt.Errorf("agentstep: %s: %w", opStart, ErrAttemptRunning)
	default:
		return rec, stored, quota, fmt.Errorf("agentstep: %s: %w", opStart, err)
	}
}

// settle charges the provider-reported usage in full and stores output (the encoded calls or FailedStepOutput) in the
// same transaction.
func (s *Service) settle(ctx context.Context, st step, id shared.ReservationID, output string, usage values.Usage) (Snapshot, error) {
	u, err := account.NewUsage(string(s.policy.Unit()), usage.Total(), false)
	if err != nil {
		return Snapshot{}, fmt.Errorf("agentstep: %s: %w", opSettle, err)
	}
	res, err := account.NewResult(output, usage.Input(), usage.Output())
	if err != nil {
		return Snapshot{}, fmt.Errorf("agentstep: %s: %w", opSettle, err)
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	now := s.clock.Now()
	var quota Snapshot
	err = s.accounts.Modify(settleCtx, s.scope(st, now, id), func(a *account.Account) error {
		if _, err := a.Complete(id, u, res, now); err != nil {
			return err
		}
		quota = snapshotOf(a.Snapshot(now))
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("agentstep: %s: %w", opSettle, err)
	}
	return quota, nil
}

// release returns the hold whole on a context detached from the client. A failure is only logged: the hold is
// released by expiry at the latest.
func (s *Service) release(ctx context.Context, st step, id shared.ReservationID, reason account.ReleaseReason) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerTimeout)
	defer cancel()
	now := s.clock.Now()
	err := s.accounts.Modify(releaseCtx, s.scope(st, now, id), func(a *account.Account) error {
		_, err := a.Release(id, reason, now)
		return err
	})
	if err != nil {
		s.log.ErrorContext(ctx, "agent step release failed", "reservation", id.String(), "op", opRelease, "err", err)
	}
}

func (s *Service) scope(st step, now time.Time, id shared.ReservationID) account.Scope {
	return account.Scope{Owner: st.owner, Policy: s.policy, Now: now, Key: st.reserve.Key(), Reservation: id}
}

func (s *Service) newID() (shared.ReservationID, error) {
	b := make([]byte, account.ReservationIDBytes)
	if err := s.random.Fill(b); err != nil {
		return "", fmt.Errorf("random reservation id: %w", err)
	}
	return account.NewReservationID(b)
}

// textReplyPrefix starts the call id of a reply made from a text answer; the reservation id after it keeps ids distinct
// within a thread (TASK-200).
const textReplyPrefix = "hub_text_"

func textReplyID(id shared.ReservationID) string { return textReplyPrefix + id.String() }

// failureReason names why a billed answer was unusable, for the log line: a snake_case form of the provider error's
// fixed reason ("no_tool_use", "output_cap", "invalid_tool_calls"), so it does not read like a lost connection.
func failureReason(err error) string {
	var pe *values.ProviderError
	if !errors.As(err, &pe) || pe.Reason == "" {
		return "unknown"
	}
	return strings.ReplaceAll(pe.Reason, " ", "_")
}

// providerFailure classifies a provider error; anything the adapter did not classify is permanent.
func providerFailure(err error) *ProviderFailedError {
	var pe *values.ProviderError
	if errors.As(err, &pe) {
		return &ProviderFailedError{Transient: pe.Kind == values.KindTransient, RetryAfter: pe.RetryAfter, Cause: pe}
	}
	return &ProviderFailedError{Cause: errors.New("unclassified provider failure")}
}
