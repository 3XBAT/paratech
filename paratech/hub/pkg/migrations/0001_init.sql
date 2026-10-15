-- Baseline of the Hub schema. It is intentionally empty: goose needs at least one migration (and go:embed at least one
-- file), and applying it creates goose's version table, so every later migration starts from a known version 1.
-- Business tables arrive with their tasks (API keys: TASK-032, quota ledger: TASK-033) as new files 0002, 0003, ...

-- +goose Up

-- +goose Down
