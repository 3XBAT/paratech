package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"unicode/utf8"

	"paratech/hub/config"
	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/pkg/prompts"
)

// agentParts are what the agent step and the server-prompt operations need besides the database: built and checked
// at New from the embedded prompts (ADR-0023), so a broken prompt, catalog, alias or seal key fails the start.
type agentParts struct {
	// sealer is nil when no seal key is configured: AgentStep is then not served.
	sealer        *seal.Sealer
	profiles      []agentstep.Profile
	serverPrompts map[values.Operation]gateway.ServerPrompt
}

// readerAliases says which configured alias each server-prompt operation runs on (ADR-0023).
func readerAliases(cfg config.Config) map[values.Operation]string {
	return map[values.Operation]string{
		values.OperationInspectMaterial:  cfg.AgentPlannerAlias,
		values.OperationAnswer:           cfg.AgentStrongAlias,
		values.OperationTriageAssignment: cfg.AgentStrongAlias,
	}
}

// opServerPromptOff is logged at start for a server-prompt operation whose alias is not in the allowlist.
const opServerPromptOff = "app: server-prompt operation unavailable, its alias is not in the model allowlist"

func newAgentParts(cfg config.Config, models *modelallowlist.List, log *slog.Logger) (*agentParts, error) {
	parts := &agentParts{serverPrompts: map[values.Operation]gateway.ServerPrompt{}}

	versions, err := loadPanel()
	if err != nil {
		return nil, fmt.Errorf("app: agent prompts: %w", err)
	}
	parts.profiles = []agentstep.Profile{{Name: values.ProfilePanel, Alias: cfg.AgentPlannerAlias, Versions: versions}}

	for op, alias := range readerAliases(cfg) {
		version, text, err := prompts.Op(string(op))
		if errors.Is(err, prompts.ErrNotFound) {
			continue // the operation stays unavailable until its prompt ships
		}
		if err != nil {
			return nil, fmt.Errorf("app: prompt of %s: %w", op, err)
		}
		if !cleanPrompt(text) {
			return nil, fmt.Errorf("app: prompt of %s v%d must be non-empty UTF-8 without NUL", op, version)
		}
		if _, _, err := models.Resolve(alias, 0); err != nil {
			// Like AgentStep without a seal key: a Hub whose allowlist lacks the alias still starts, and the
			// operation is refused as unavailable. The warning tells the operator why.
			log.Warn(opServerPromptOff, slog.String("operation", string(op)), slog.String("alias", alias))
			continue
		}
		reask, err := loadReask(op, version)
		if err != nil {
			return nil, err
		}
		parts.serverPrompts[op] = gateway.ServerPrompt{
			Alias: alias, Version: "v" + strconv.FormatUint(uint64(version), 10), Text: string(text), Reask: reask,
		}
	}

	keys, err := cfg.SealKeys()
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	if len(keys) == 0 {
		return parts, nil
	}
	if _, _, err := models.Resolve(cfg.AgentPlannerAlias, 0); err != nil {
		return nil, fmt.Errorf("app: %s %q: %w", config.EnvAgentPlannerAlias, cfg.AgentPlannerAlias, err)
	}
	sealKeys := make([]seal.Key, 0, len(keys))
	for _, k := range keys {
		sealKeys = append(sealKeys, seal.Key{Version: k.Version, Secret: k.Secret})
	}
	if parts.sealer, err = seal.New(sealKeys[0], sealKeys[1:]...); err != nil {
		return nil, fmt.Errorf("app: %s: %w", config.EnvAgentSealKeys, err)
	}
	return parts, nil
}

// loadReask returns the reask note of the prompt version of op, "" when that version has none.
func loadReask(op values.Operation, version uint32) (string, error) {
	v, note, err := prompts.OpReask(string(op))
	if errors.Is(err, prompts.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("app: reask note of %s: %w", op, err)
	}
	if v != version || !cleanPrompt(note) {
		return "", fmt.Errorf("app: reask note of %s v%d must be non-empty UTF-8 without NUL of the prompt version", op, v)
	}
	return string(note), nil
}

// loadPanel parses every embedded version of the panel profile; the catalog version must match its directory.
func loadPanel() ([]agentstep.Version, error) {
	embedded, err := prompts.Agent(string(values.ProfilePanel))
	if err != nil {
		return nil, err
	}
	out := make([]agentstep.Version, 0, len(embedded))
	for _, v := range embedded {
		catalog, err := toolcatalog.Parse(v.Tools)
		if err != nil {
			return nil, fmt.Errorf("panel v%d: %w", v.Version, err)
		}
		if catalog.Version() != v.Version {
			return nil, fmt.Errorf("panel v%d: the catalog says version %d", v.Version, catalog.Version())
		}
		if !cleanPrompt(v.System) {
			return nil, fmt.Errorf("panel v%d: the system prompt must be non-empty UTF-8 without NUL", v.Version)
		}
		out = append(out, agentstep.Version{System: string(v.System), Catalog: catalog})
	}
	return out, nil
}

func cleanPrompt(b []byte) bool {
	if len(b) == 0 || !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}
