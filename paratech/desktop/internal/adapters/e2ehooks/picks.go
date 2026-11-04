package e2ehooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Bounds of the pick queue: the runner writes a few short lines.
const (
	maxQueueBytes = 1 << 20  // picks.jsonl and consumed.jsonl
	maxLineBytes  = 64 << 10 // one line of picks.jsonl
	maxPaths      = 64       // paths of one line
)

// Results of a consumed.jsonl line.
const (
	resultPicked    = "picked"
	resultCancelled = "cancelled"
)

// pickLine is a line of picks.jsonl.
type pickLine struct {
	Dialog string   `json:"dialog"`
	Paths  []string `json:"paths"`
}

// consumedLine is a line of consumed.jsonl.
type consumedLine struct {
	Line   int    `json:"line"` // 1-based, of picks.jsonl
	Dialog string `json:"dialog"`
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
}

// PickFile answers a single-file open dialog with the next line of picks.jsonl: the resolved path, or "" (cancelled).
// It never fails: every refusal is a cancel and an error line in the log.
func (h *Hooks) PickFile(_ context.Context, dialog string) (string, error) {
	paths := h.pick(dialog, false)
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

// PickFiles answers an open dialog of one file or, when multiple, several, like PickFile; none is cancelled.
func (h *Hooks) PickFiles(_ context.Context, dialog string, multiple bool) ([]string, error) {
	return h.pick(dialog, multiple), nil
}

// pick takes the next unused line, checks it for dialog and records it as used; nil when cancelled.
func (h *Hooks) pick(dialog string, multiple bool) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	used, err := h.readSmall(consumedFile, maxQueueBytes)
	if err != nil {
		h.log.Error(opPick, "dialog", dialog, "result", resultCancelled, "reason", "consumed record unreadable", "err", err)
		return nil
	}
	n := bytes.Count(used, []byte{'\n'})
	queue, err := h.readSmall(picksFile, maxQueueBytes)
	if err != nil {
		h.log.Error(opPick, "dialog", dialog, "result", resultCancelled, "reason", "pick queue unreadable", "err", err)
		return nil
	}
	lines := splitLines(queue)
	if n >= len(lines) {
		h.log.Error(opPick, "dialog", dialog, "result", resultCancelled, "reason", "no pick left in the queue",
			"used", n)
		return nil
	}

	paths, reason := h.check(lines[n], dialog, multiple)
	rec := consumedLine{Line: n + 1, Dialog: dialog, Result: resultPicked}
	if reason != "" {
		rec.Result, rec.Reason, paths = resultCancelled, reason, nil
	}
	// Not recorded: not used either, so the queue does not drift from the record.
	if err := h.appendJSON(consumedFile, rec); err != nil {
		h.log.Error(opPick, "dialog", dialog, "line", rec.Line, "result", resultCancelled,
			"reason", "consumed record not written", "err", err)
		return nil
	}
	if reason != "" {
		h.log.Error(opPick, "dialog", dialog, "line", rec.Line, "result", resultCancelled, "reason", reason)
		return nil
	}
	h.log.Info(opPick, "dialog", dialog, "line", rec.Line, "result", resultPicked, "files", len(paths))
	return paths
}

// splitLines splits the queue into its complete lines, those ending in '\n' ("\r\n" is accepted). A final fragment
// without '\n' may be a line the runner is still writing: it is not a line yet, so it is neither picked nor consumed.
func splitLines(b []byte) []string {
	end := bytes.LastIndexByte(b, '\n')
	if end < 0 {
		return nil
	}
	lines := strings.Split(string(b[:end]), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// check decodes line and resolves its paths for dialog; a non-empty reason means cancelled.
func (h *Hooks) check(line, dialog string, multiple bool) ([]string, string) {
	if len(line) > maxLineBytes {
		return nil, fmt.Sprintf("the line is longer than %d bytes", maxLineBytes)
	}
	var p pickLine
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, "the line is not a pick: " + err.Error()
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, "the line has more than one value"
	}
	switch {
	case p.Dialog != dialog:
		return nil, fmt.Sprintf("the line is for dialog %q", p.Dialog)
	case len(p.Paths) == 0:
		return nil, "the line has no paths"
	case !multiple && len(p.Paths) > 1:
		return nil, "several paths for a single-file dialog"
	case len(p.Paths) > maxPaths:
		return nil, fmt.Sprintf("more than %d paths", maxPaths)
	}
	out := make([]string, 0, len(p.Paths))
	for i, rel := range p.Paths {
		path, err := h.resolveFixture(rel)
		if err != nil {
			return nil, fmt.Sprintf("path %d: %v", i+1, err)
		}
		out = append(out, path)
	}
	return out, ""
}
