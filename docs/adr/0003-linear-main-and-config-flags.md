# ADR-0003: Linear `main` and flag parsing in `config`

- **Status:** Accepted
- **Source:** review of PR #2 (TASK-002)

## Context
The first `cmd/<binary>/main.go` kept flag parsing, config loading, logger setup and exit-code mapping in a 55-line `run()`
(pointers from the `flag` package, no visual grouping). Review asked for a `main` that reads as a short sequence of named steps
and delegates initialization to `app`, following the Uber and Google Go style guides ("exit once", no logic or globals in `init`).

## Decision
- `main()` is a linear sequence: `config.MustLoad` -> `setupLogger` -> `app.New` -> `Run` until a signal -> `Close`, with blank lines between steps.
- Command-line flags are parsed in `config.Load(args, out, getenv)`. Priority: flags, environment, defaults. `config.MustLoad` is the entry-point wrapper:
  it exits with status 0 for `--help` and 2 for a bad flag or configuration. `os.Exit` is therefore called only by `main` and `config.MustLoad`.
- `ARCH-51` and `ARCH-52` in `docs/architecture.md` are updated accordingly. `config` still imports only the standard library.
- The `--version` and `--check` flags of the first scaffold are removed (not required by TASK-002); they can return when a task needs them.

## Consequences
- `cmd/*/main.go` has no unit tests of its own; flag and help behavior is covered by `config` tests.
- The architecture test is unchanged: `cmd` may still import only `config` and `internal/app` (plus std and third-party).
