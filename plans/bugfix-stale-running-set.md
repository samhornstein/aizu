# Fix: crash mid-task permanently blocks an issue

**Branch:** `fix/stale-running-set`
**PR title:** `fix: recover stale queue state on startup`

## Context

Aizu's Redis work queue (`internal/queue/queue.go`) deduplicates per issue/PR:
`Enqueue` atomically refuses a new task if `repo#number` is in the
`aizu:queued` or `aizu:running` sets. `NextPending` moves the key from
`queued` to `running`; `MarkDone`/`MarkFailed` clear it.

**The bug:** nothing ever clears `aizu:running` on startup. If the process
crashes (or is killed, or the machine reboots) while a task is running, that
`repo#number` stays in `aizu:running` forever. Every future trigger for that
issue is silently skipped by `enqueueScript`. The user sees Aizu ignore an
issue with no error anywhere.

There is precedent for startup recovery: `main.go` calls
`exec.CleanupStale()` to remove leftover agent containers — but the matching
queue state is never cleaned.

A second, smaller leak: `NextPending` pops a task ID, then `GET`s the task
JSON. If the JSON has expired (24h TTL), it logs "Task expired or missing" and
returns — but the `aizu:queued` entry for that task can never be removed
because the repo/number is only known from the JSON. That entry then blocks
the issue until something else clears it.

## Approach

Add a `RecoverStale` method to `Queue` and call it at worker startup,
alongside `exec.CleanupStale()`.

### Steps

1. In `internal/queue/queue.go`, add:

   ```go
   // RecoverStale clears queue state that can only be left behind by a previous
   // process: the running set (tasks die with the process that ran them) and
   // queued-set entries whose task JSON has expired. Call once at startup,
   // before any worker goroutine begins consuming.
   func (q *Queue) RecoverStale(ctx context.Context) {
       if n, err := q.rdb.Del(ctx, runningKey).Result(); err != nil {
           slog.Warn("Could not clear running set", "error", err)
       } else if n > 0 {
           slog.Info("Cleared stale running set from previous run")
       }
   }
   ```

   Deleting the whole `runningKey` is correct because all workers live in this
   one process (true today with a single worker, and still true after
   `feat-worker-concurrency.md`, which adds goroutines, not processes). If
   multi-*process* workers are ever added, this must change to per-worker
   leases — leave a comment saying so.

2. Fix the queued-set leak by making the ID list self-describing. In `Task`
   the repo/number are already fields; the problem is only the expired-JSON
   case. Simplest robust fix: when `NextPending` finds the task JSON missing,
   rebuild nothing — instead store `repo#number` in the queue list entry
   itself. Change the list payload from `task.ID` to `task.ID + "|" + activeKey(repo, number)`:

   - `enqueueScript`: `ARGV[1]` becomes the combined payload; adjust the
     `LPUSH` line (the script already receives it as an opaque string, so only
     the Go call sites change what they pass).
   - `push`: LPush the combined payload.
   - `NextPending`: split on the first `|`; on expired/missing JSON, `SRem`
     the queued set with the recovered key before returning.

   Keep the separator parsing tolerant: if there is no `|` (old-format entry
   left in Redis across an upgrade), fall back to current behavior.

3. In `main.go`, inside the worker-mode block (next to `exec.CleanupStale()`):

   ```go
   q.RecoverStale(ctx)
   ```

   Note `q` is created before the mode blocks, so it is in scope.

### Deliberate non-goals of the recovery

Tasks that were mid-run when the process died are **dropped**, not re-queued:
the triggering comment already got its "eyes" reaction, and re-running a
half-finished agent job unprompted is worse than requiring the user to
comment again. Log at Info level so the operator can see it happened.

## Files to modify

- `internal/queue/queue.go`
- `main.go`

## Tests

Create `internal/queue/queue_test.go` using
`github.com/alicebob/miniredis/v2` (`go get github.com/alicebob/miniredis/v2`;
construct with `New("redis://" + mr.Addr())`). Cover:

1. **Recovery unblocks**: Enqueue → NextPending (task now "running") →
   simulate crash by just constructing a new Queue on the same miniredis →
   `Enqueue` same repo#number is skipped (returns nil,nil) → `RecoverStale` →
   `Enqueue` succeeds.
2. **Expired-JSON cleanup**: Enqueue, then delete the `aizu:task:<id>` key
   directly via miniredis, call `NextPending` → returns (nil, nil) and the
   queued set no longer contains the key → `Enqueue` for the same issue
   succeeds.
3. **Round-trip sanity**: Enqueue → NextPending returns the same task fields →
   MarkDone → sets empty.
4. **Old-format list entry** (no `|`): LPush a bare ID manually; NextPending
   must not panic and must still return the task if its JSON exists.

## Verification

```sh
make build
go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: `docker compose up -d`, trigger a task, `docker compose kill aizu`
while the agent runs, `docker compose up -d` again, re-trigger the same issue
with a new comment — it must run (before this fix it is silently ignored).

## Out of scope

- No re-queueing of interrupted tasks.
- No multi-process worker support / leases.
- Do not change retry or dead-letter behavior.
