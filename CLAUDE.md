# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Daily automated pprof profiler for `jago-service`. Runs at peak hours (Asia/Jakarta timezone), fetches CPU + heap profiles, analyzes via Claude Opus, persists to SQLite, uploads to GitHub, reports to Slack.

Status: incremental build per `PLAN.md`. Steps 1–6 done; Railway deploy + retry/health-check pending.

## Commands

```bash
go build ./...                       # compile
go run .                             # run scheduler (blocks until SIGINT)
go test ./...                        # all tests
go test -run TestIntegration -v      # single test
go mod tidy                          # sync deps
```

Requires `go tool pprof` on PATH (used via `exec.Command` in `profiler.go:137`).

## Required env vars

- `JAGO_SERVICE_URL` (default `https://jago-service.jagocoffee.dev`)
- `ANTHROPIC_API_KEY` — Opus analysis
- `SLACK_BOT_TOKEN`, `SLACK_CHANNEL_ID` — notifications (skipped silently if unset)
- `GITHUB_TOKEN` — profile upload (skipped if unset)
- `DB_PATH` (default `profiler.db`)
- `TZ=Asia/Jakarta` for cron alignment

## Architecture

Single `main` package + `storage/` subpackage. Pipeline:

```
cron (main.go) → fetchProfile (profiler.go) → buffer in profiles map
              → on sample 3: analyzeProfiles (analyzer.go)
                           → SaveProfile (storage/sqlite.go)
                           → uploadProfilesToGitHub (github.go)
                           → sendSlackReport (notifier.go)
```

**Sampling model:** each peak window fires 3 cron jobs (start/mid/end). `runProfiler` (`main.go:82`) appends each `*Profile` to a shared `profiles[window]` slice guarded by `profilesMu`. Only sample 3 triggers `analyzeAndReport`, which expects exactly 3 buffered profiles. Drift in cron firing or a missed sample leaves a stale slice — the next end-sample run for that window will see a wrong count and error.

**Windows:** `morning` (07:00, 07:30, 08:00) and `afternoon` (12:00, 12:30, 13:00) Asia/Jakarta. Defined inline in `main.go:44` — keep `schedule`/`window`/`sampleNum` consistent if editing.

**Yesterday comparison:** `analyzer.go:37` calls `GetProfileByTimestamp(today.AddDate(0,0,-1))`. Lookup uses exact timestamp equality on `run_timestamp` (`storage/sqlite.go:91`) — must match a prior sample's `profiles[0].Timestamp` exactly. Currently best-effort; missing yday data is logged and analysis proceeds.

**Opus call:** `analyzer.go:111` hits `api.anthropic.com/v1/messages` directly (no SDK), model `claude-opus-4-1-20250805`. Response parsed by extracting first `{` to last `}` (`parseOpusResponse`) — fragile if Opus prepends prose containing braces.

**GitHub upload:** PUT to `/repos/jagocoffee/profiler-service/contents/...` (`github.go:65`). Repo hardcoded. Uploads only the `.txt` (text-converted) profiles, not raw `.prof` binaries.

**SQLite:** schema in `storage/sqlite.go:25`. `run_timestamp` is UNIQUE, so re-saving the same instant fails. Package uses a global `db *sql.DB` — call `storage.Init` before any other storage func.

## Conventions

- All non-storage code lives in `package main` at repo root (flat layout, no `internal/`).
- Errors that should not abort the pipeline (Slack/GitHub failures) log and continue; errors that should (fetch, analyze, save) call `sendSlackError` and return.
- Profile filename pattern: `profiles/YYYY-MM-DD-HHMM-{window}-{sampleNum}-{cpu|heap}.{prof|txt}` — see `profiler.go:55`.

## Testing

`test_integration_test.go` exercises storage + JSON round-trip with fake `*Profile` data (no network). No mock for HTTP/Opus/Slack/GitHub yet — keep external calls behind functions that can be swapped if adding tests.
