# Feature: feedback-loop polish — status reactions, help, progress, rate limit

**Branch:** `feat/feedback-polish`
**PR title:** `feat: completion reactions, help command, worker progress comment, rate limit`

Four independent sections sharing a theme: the user should always know what
Aizu is doing without reading server logs. Each section stands alone; if
one conflicts with in-flight work, drop it and note it in the PR.

## Context (shared)

The worker (`internal/worker/worker.go`) adds an 👀 reaction when it picks
up a comment-triggered task, then goes silent for up to an hour
(`AIZU_TIMEOUT`) until the final reply lands. Failures reply with an error
comment; success replies with agent output. There is no completion signal
on the triggering comment, no usage discovery, no liveness signal during
the run, and no cap on how often the agent can run.

## 1. Completion reactions

**What:** on success add 🚀 (`rocket`), on failure add 😕 (`confused`) to
the triggering comment, alongside the existing 👀.

**How:** in `worker.process()`, after the success reply call
`w.gh.AddReaction(ctx, task.Repo, task.CommentID, "rocket")`; in the
failure path use `"confused"` (both only when `task.CommentID > 0`, same
guard as the existing eyes reaction; wrap in the same
"Could not add reaction" Warn handling). `AddReaction` in
`internal/github/client.go` already supports arbitrary content strings —
valid values: `+1 -1 laugh confused heart hooray rocket eyes`.

**Test:** worker test asserting the fake server's reactions endpoint
receives `eyes` then `rocket` on success, `eyes` then `confused` on a
failing engine (mock executor returning exit 1).

## 2. `aizu help` command

**What:** a comment that is exactly the trigger word plus `help` (e.g.
`aizu help`) gets an immediate usage reply from the worker itself — no
sandbox, no engine, no model tokens.

**How:** in `worker.process()`, before `handle()`:

```go
if isHelpRequest(task.Body, w.trigger) { // strings.TrimSpace + strings.EqualFold on the remainder
    w.reply(ctx, task, helpText(w.trigger), log)
    w.q.MarkDone(ctx, task)
    return
}
```

The Worker doesn't know the trigger word today — pass it (or the whole
`*config.Config`) into `worker.New` from `main.go`. `helpText` returns a
short markdown block: what Aizu is, example commands
(`<trigger> implement this`, `<trigger> review this PR`,
`<trigger> help`), and a pointer to the docs site. Keep it under ~15 lines.
Detect in the worker rather than the poller so help requests still flow
through the queue's dedupe (and so an empty command — bare `aizu` — can
also produce help: treat "trigger word with nothing after it" the same
way).

**Test:** worker test: body `aizu help` → reply contains "implement", no
executor call (assert mock executor's Create was not invoked); bare `aizu`
behaves the same; `aizu helpme do X` is *not* help (goes to the engine).

## 3. Worker-managed progress comment

**What:** the moment a task starts, the worker posts
`⏳ Aizu is working on this…` and then **edits that same comment** into the
final result (success or failure). The user gets instant acknowledgment in
the thread, there's exactly one result comment, and it works even when the
agent model is too small to follow AIZU.md's do-it-yourself progress
instructions.

**How:**
- `internal/github/client.go`: `CreateComment` currently returns only
  `error`; add the ID. Change signature to
  `CreateComment(ctx, repoFull, number, body) (int64, error)` — decode the
  response (`{"id": …}`) via the existing `post(ctx, u, body, &out)`
  plumbing. Add
  `UpdateComment(ctx context.Context, repoFull string, commentID int64, body string) error`
  → `PATCH /repos/{o}/{r}/issues/comments/{id}`. `Client` has no PATCH
  helper; generalize `post` into
  `send(ctx, method, url, body, out)` and keep `post` as a one-line wrapper.
- `worker.process()`: post the placeholder right after the eyes reaction,
  keep the returned ID, and change `reply` to
  "update placeholder if we have an ID, else create" (fall back to creating
  when the placeholder post failed — never lose a result).
- Interaction with `feat-single-account-mode.md`: the marker is appended in
  `CreateComment`; apply the same append inside `UpdateComment` so the
  final edited body still carries it.
- Update `AIZU.md`'s Progress Updates section: the agent should still post
  its *own checklist* comment for long tasks (that's richer), but the
  worker's placeholder is the reliability floor. If this makes the prompt
  too long for small models, simplify to: agent checklists optional.

**Test:** worker test: fake server records a POST (placeholder, body
contains "working") then a PATCH to the returned ID whose body equals the
engine output; failure path PATCHes the error text; when the POST endpoint
returns 500, the final result still arrives as a new POST.

## 4. Rate limit

**What:** cap agent runs per repo per hour; a tripped limit replies once
with "rate limit reached, try again after HH:MM". Insurance against
trigger loops (see `bugfix-issue-retrigger-loop.md` — this is the
belt-and-suspenders for that class of bug) and against a hijacked/spammy
thread burning a day of GPU time.

**How:**
- Config: `MaxRunsPerHour int` default `10`, env `AIZU_MAX_RUNS_PER_HOUR`
  (0 disables). Add to `.env.example`.
- Queue (`internal/queue/queue.go`) — it owns Redis:

  ```go
  // AllowRun increments the hourly run counter for repo and reports whether
  // the run is within limit. The window is a fixed hour bucket.
  func (q *Queue) AllowRun(ctx context.Context, repo string, limit int) bool {
      if limit <= 0 { return true }
      key := fmt.Sprintf("aizu:rate:%s:%s", repo, time.Now().UTC().Format("2006010215"))
      n, err := q.rdb.Incr(ctx, key).Result()
      if err != nil { return true } // fail open: availability over strictness here
      q.rdb.Expire(ctx, key, 2*time.Hour)
      return n <= int64(limit)
  }
  ```

- Worker: check in `process()` before creating the sandbox; on deny, reply
  once (the placeholder-edit machinery from section 3 makes this clean) and
  `MarkDone` (do not retry/dead-letter). To avoid a reply *per* excess
  trigger, only post the limit message when `n == limit+1` — have
  `AllowRun` return the count and let the worker decide.
- The worker needs the limit — comes with the same `config` plumbed in for
  section 2.

**Test:** miniredis-backed queue test: limit 2 → third `AllowRun` false,
counter expires (use `mr.FastForward(2 * time.Hour)`); worker test: third
task gets the limit reply and no executor call, fourth gets no reply at
all.

## Files to modify

- `internal/worker/worker.go` (+ `worker_test.go`) — all four sections
- `internal/github/client.go` (+ `client_test.go`) — sections 1, 3
- `internal/queue/queue.go` (+ `queue_test.go`) — section 4
- `internal/config/config.go` (+ test), `.env.example` — sections 2, 4
- `main.go` — worker.New signature
- `AIZU.md` — section 3
- `e2e/e2e_test.go` — CreateComment signature change ripples into the fake;
  also the pipeline test now sees placeholder-then-PATCH

## Verification

```sh
make build && go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: trigger a real task and watch the thread: 👀 appears, the ⏳
comment appears within a second, it becomes the result, and 🚀 lands on
your comment. Then `aizu help` → instant usage reply. Then set
`AIZU_MAX_RUNS_PER_HOUR=1` and trigger twice → second gets the limit
message.

## Out of scope

- Streaming/incremental progress from inside the engine run.
- Per-user rate limits (per-repo only).
- Notification channels outside GitHub (no email/Slack).
