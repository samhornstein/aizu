# Feature: configurable worker concurrency

**Branch:** `feat/worker-concurrency`
**PR title:** `feat: run multiple agent tasks concurrently via AIZU_CONCURRENCY`

## Context

Aizu's worker (`internal/worker/worker.go`) is strictly serial: one
goroutine pops tasks from Redis (`Queue.NextPending`, a blocking `BRPOP`)
and runs the agent to completion before taking the next. Agent runs take
minutes to an hour (`AIZU_TIMEOUT` default 3600s), so a second trigger —
even on a different repo — waits behind the first. For a single user this
is tolerable; for a team sharing one Aizu instance it's the first thing
they'll hit.

**Why it's safe to parallelize:** the Worker struct is stateless across
tasks; each task gets its own sandbox container (`executor.Create` generates
a fresh `aizu-<uuid>` container). The queue already serializes access:
`BRPOP` delivers each task to exactly one consumer, and the enqueue Lua
script's queued/running-set dedupe guarantees at most one active task per
issue/PR regardless of worker count. Redis `go-redis` clients are
goroutine-safe.

**Interaction with `fix/stale-running-set`:** that plan clears the whole
`aizu:running` set at startup, which assumes all workers live in one
process. This plan adds goroutines, not processes, so that assumption
holds. Do not run multiple Aizu processes against one Redis in worker mode.

## Approach

Add an `AIZU_CONCURRENCY` setting (default 1) and start that many worker
goroutines.

### Steps

1. **Config.** In `internal/config/config.go`:
   - Add `Concurrency int` to the `Config` struct under the Agent section,
     with a doc comment: `// number of agent tasks run in parallel`.
   - Default `Concurrency: 1` in `Load()`.
   - Env override, following the existing pattern:

     ```go
     if n, ok := envInt("AIZU_CONCURRENCY"); ok && n > 0 {
         cfg.Concurrency = n
     }
     ```

   - Add a test in `internal/config/config_test.go` mirroring the existing
     env-override tests (`t.Setenv`), covering default, valid override, and
     ignored values (`0`, `-1`, non-numeric).

2. **Startup.** In `main.go`, replace the single worker goroutine with a
   loop:

   ```go
   w := worker.New(q, exec, gh, loader)
   for i := 0; i < cfg.Concurrency; i++ {
       wg.Add(1)
       go func() {
           defer wg.Done()
           w.Run(ctx)
       }()
   }
   slog.Info("Worker started", "concurrency", cfg.Concurrency)
   ```

   One shared Worker value across goroutines is fine (verify it stays
   field-read-only per task; it is today). Keep `exec.CleanupStale()` (and
   `q.RecoverStale` if present) **before** the loop, exactly once.

3. **Resource note.** Each concurrent task launches a container with
   `--memory=4g --cpus=2` (`internal/executor/container.go`). Document in
   `.env.example` next to the new setting:

   ```
   # AIZU_CONCURRENCY=1                         # agent tasks run in parallel; each uses up to 4g RAM / 2 CPUs
   ```

4. **Docs.** Mention the setting in `docs/content/docs/getting-started/` or
   best-practices if there is a configuration table (grep for
   `POLL_INTERVAL` to find where settings are listed; keep formatting
   consistent).

## Files to modify

- `internal/config/config.go`
- `internal/config/config_test.go`
- `main.go`
- `.env.example`
- `docs/content/docs/**` (settings listings, if present)

## Tests

- Config tests as above.
- Concurrency behavior test (optional but valuable), in `e2e/e2e_test.go` or
  a worker test with miniredis: start 2 worker goroutines with a mock
  executor whose `RunEngine` blocks on a channel; enqueue tasks for two
  *different* issues; assert both executors are inside `RunEngine`
  simultaneously (e.g. a `sync.WaitGroup`/counter with a timeout), then
  release. Also assert two tasks for the *same* issue never run
  concurrently (second enqueue is rejected while the first is active —
  already covered by queue dedupe, cheap to assert here).
- Run everything with `-race` (CI already does).

## Verification

```sh
make build
go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: set `AIZU_CONCURRENCY=2`, trigger `aizu …` on two different issues
within one poll interval, and confirm via `docker ps` that two `aizu-*`
agent containers run at once and both issues get replies.

## Out of scope

- Multi-process / multi-host workers (would require per-worker leases in
  the queue).
- Per-repo concurrency limits or prioritization.
- Changing container resource limits.
