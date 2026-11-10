package hubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const opSummary = "hubclient: chat summary"

// Summarize asks the Hub for the summary of earlier requests of a chat (OPERATION_TYPE_CHAT_SUMMARY, ADR-0028; the
// agentcompact.Summarizer port) with the URL, key, limits and error classes of Complete, unary and bounded by
// RequestTimeout (the Hub bounds the provider call). It sends no instructions, no model alias and no output cap: the
// Hub uses its own prompt, alias and cap. It returns the summary text, untrusted model output that only
// values.ParseHistorySummary may accept. A permanent ErrProvider (the Hub's charged refusal, replayed for the same key)
// and an answer that is not exactly {"summary":"<text>"} also match agent.ErrSummaryRefused.
func (c *Client) Summarize(ctx context.Context, req agent.SummaryRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", opSummary, err)
	}
	if req.IsZero() {
		return "", permanent(opSummary, ErrInvalidRequest)
	}
	base, err := c.baseURL(ctx, opSummary)
	if err != nil {
		return "", err
	}
	key, err := c.apiKey(ctx, opSummary)
	if err != nil {
		return "", err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: req.Key(),
		OperationType:  hubv1.OperationType_OPERATION_TYPE_CHAT_SUMMARY,
		UntrustedData:  summaryData(req),
	}})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(callCtx, creq)
	if err != nil {
		if perr := callerEnd(ctx); perr != nil {
			return "", fmt.Errorf("%s: %w", opSummary, perr)
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", unavailable(opSummary, ErrTimeout)
		}
		e := mapCallError(opSummary, err)
		if errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
			e.extra = append(e.extra, agent.ErrSummaryRefused)
		}
		return "", e
	}
	res := resp.Msg.GetResult()
	if res == nil {
		return "", permanent(opSummary, ErrBadResponse)
	}
	text, ok := summaryOf(res.GetOutput())
	if !ok {
		e := permanent(opSummary, ErrBadResponse)
		e.extra = []error{agent.ErrSummaryRefused}
		return "", e
	}
	return text, nil
}

type summaryPair struct {
	Student string `json:"student"`
	Reply   string `json:"reply"`
}

// summaryData is the untrusted data of the Hub's chat summary reader: {"summary":…,"pairs":[{"student":…,
// "reply":…}]} without HTML escaping, as the reader re-encodes it; its size is agent.SummaryEncodedSize (tested).
func summaryData(req agent.SummaryRequest) string {
	pairs := make([]summaryPair, 0, len(req.Pairs()))
	for _, p := range req.Pairs() {
		pairs = append(pairs, summaryPair(p))
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings: encoding cannot fail.
	_ = enc.Encode(struct {
		Summary string        `json:"summary"`
		Pairs   []summaryPair `json:"pairs"`
	}{req.Previous(), pairs})
	return strings.TrimSuffix(b.String(), "\n")
}

// summaryOf reads the Hub's canonical answer: exactly one JSON object with the one key summary (exact case, not
// repeated), a string, and nothing after it. It reads tokens, as a map would keep the last of repeated keys.
func summaryOf(output string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(output))
	for _, want := range []any{json.Delim('{'), "summary"} {
		if tok, err := dec.Token(); err != nil || tok != want {
			return "", false
		}
	}
	tok, err := dec.Token()
	text, ok := tok.(string)
	if err != nil || !ok {
		return "", false
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return "", false // another key, repeated or not
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", false // trailing data
	}
	return text, true
}
