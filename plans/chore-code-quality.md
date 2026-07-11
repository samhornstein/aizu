# Chore: small cleanups and missing tests

**Branch:** `chore/code-quality`
**PR title:** `chore: dedupe model discovery, drop dead Dockerfile copy, test queue, skip closed issues`

Four independent small items; one branch is fine. Each section stands alone —
if one conflicts with another in-flight plan, drop it and note it in the PR.

## 1. Deduplicate model discovery

**Context:** `internal/executor/container.go` calls `discoverModelID` (an
HTTP GET to `<OPENAI_BASE_URL>/models`) twice per task: once in
`writeModelsJSON` (called from `Create`) and again in `RunEngine`. Two
network calls where one suffices, and the second failure mode is silent
(`if err == nil` guard) while the first is fatal — inconsistent.

**Approach:** discover once per task in `Create`, store the result on the
executor keyed by sandbox id, reuse in `RunEngine`, and delete the entry in
`Destroy`.

```go
type containerExecutor struct {
    cfg *config.Config
    mu  sync.Mutex
    models map[string]string // sid -> discovered model id
}
```

- `New` (in `executor.go`) initializes the map.
- `Create`: after `writeModelsJSON` succeeds, record the id (have
  `writeModelsJSON` return `(string, error)` or discover before calling it
  and pass the id in).
- `RunEngine`: read from the map instead of calling `discoverModelID`.
- `Destroy`: delete the map entry.
- Guard map access with the mutex (safe under future concurrency).

## 2. Remove dead Dockerfile COPY

**Context:** the root `Dockerfile` runtime stage has
`COPY AIZU.md /opt/aizu/AIZU.md`, but `main.go` embeds AIZU.md via
`//go:embed` — nothing reads `/opt/aizu/AIZU.md`. Confirm with
`grep -r "/opt/aizu" --include="*.go" .` (expect no hits), then delete the
COPY line.

## 3. Unit tests for the queue package

**Context:** `internal/queue` has no tests; it holds the trickiest logic in
the codebase (atomic enqueue dedupe via Lua, retry/dead-letter). It is only
exercised indirectly by `e2e/e2e_test.go`, which needs real Redis.

**Approach:** add `internal/queue/queue_test.go` using
`github.com/alicebob/miniredis/v2` (`go get github.com/alicebob/miniredis/v2`;
`q := New("redis://" + mr.Addr())`). Note miniredis supports EVAL/Lua.
Cover:

- Enqueue → NextPending round-trips all Task fields.
- Duplicate Enqueue for the same repo#number while queued → returns
  `(nil, nil)`; different issue numbers are independent.
- After MarkDone, the same issue can be enqueued again.
- MarkFailed under the retry limit re-queues (task comes back from
  NextPending with Retries=1); at the limit it lands in `aizu:failed` and is
  not re-queued. Note `retryDelay` is 5s — to keep the test fast, either
  accept one 5s wait in a single test, or (better) make `retryDelay` a
  package-level `var` so the test can shrink it.
- NextPending times out with `(nil, nil)` on an empty queue. Caveat:
  miniredis's `BRPop` with a live server works, but its clock does not
  auto-advance — use `mr.FastForward` if the blocking timeout doesn't fire;
  if that fights the test, use a very short timeout (e.g. 50ms) and accept
  real waiting.

(If `bugfix-stale-running-set.md` landed first, it already created this file
— extend it instead of recreating, and skip duplicated cases.)

## 4. Skip closed issues/PRs in the poller

**Context:** `internal/github/client.go` `ListIssues` requests
`state=all` (its doc comment even claims "Only open issues are returned" —
false), and the comment path never checks state either: commenting `aizu …`
on a closed issue runs the agent. Usually unwanted.

**Approach:**
- In `ListIssues`, change `q.Set("state", "all")` to `q.Set("state", "open")`
  and fix the doc comment.
- In the worker (`internal/worker/worker.go` `handle()`), after `GetIssue`,
  return a friendly error (or skip silently — pick skip + log at Info) when
  `issue.State == "closed"`. This covers the comment path without another
  API call, since `GetIssue` already returns `state`.
- Test: worker test with a fake server returning `"state": "closed"` →
  no engine run, no reply posted (assert the fake server's comment endpoint
  was not hit; log-only). Poller side needs no test beyond compilation — the
  query-param change is covered by `client_test.go` if it asserts params
  (check; extend if trivial).

## Files to modify

- `internal/executor/executor.go`, `internal/executor/container.go`
- `Dockerfile`
- `internal/queue/queue_test.go` (new), `go.mod`/`go.sum`
- `internal/github/client.go`, `internal/worker/worker.go`,
  `internal/worker/worker_test.go`

## Verification

```sh
make build
go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
docker build -t aizu:check .   # Dockerfile still builds after the COPY removal
```

## Out of scope

- No behavior changes beyond item 4's closed-issue skip.
- Do not refactor the executor interface or add context threading.
