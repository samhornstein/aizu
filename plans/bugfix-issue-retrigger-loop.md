# Fix: issue-body triggers re-run the agent in an infinite loop

**Branch:** `fix/issue-retrigger-loop`
**PR title:** `fix: prevent issue-body and edited-comment triggers from re-enqueueing`

## Context

Aizu polls GitHub for comments and issues that begin with a trigger keyword
(default `aizu`), enqueues a task to Redis, and a worker runs a coding agent
that posts a reply comment. The poller lives in `internal/poller/poller.go`;
the queue in `internal/queue/queue.go`.

There are two trigger paths:

1. **Comments** — `pollRepo` lists issue comments updated since a Redis cursor
   (`aizu:since:<repo>`).
2. **Issue bodies** — `pollIssues` (poller.go, ~line 99) lists issues updated
   since a second cursor (`aizu:issuesSince:<repo>`) and triggers when the
   *issue body* starts with the keyword. It enqueues with `CommentID=0`.

**The bug:** the issue-body path loops forever.

- `pollIssues` saves the cursor as the latest returned issue's `updated_at`
  (poller.go, `saveIssuesSince(ctx, repo, latest)`), and GitHub's `since`
  parameter is **inclusive** (`updated_at >= since`), so the newest issue is
  returned again on every subsequent poll.
- Worse: when the worker finishes, the bot posts a reply comment, which bumps
  the issue's `updated_at` *past* the cursor. The issue body still starts with
  the trigger keyword, so it re-triggers.
- The only dedupe is the queue's atomic check against the `aizu:queued` /
  `aizu:running` sets (`enqueueScript` in queue.go), which guards **only while
  a task is active**. Once the task completes, `MarkDone` clears those sets.

Net effect: create an issue whose body starts with `aizu …` → the agent runs,
replies, the issue is re-fetched next poll, re-enqueued, runs again — forever,
burning model tokens and spamming the issue.

A milder variant exists on the comment path: `ListIssueComments` also filters
by `updated_at`, so a user **editing** an old triggering comment re-runs the
agent. That may even be desirable ("edit to re-run"), but an edit that doesn't
touch the trigger line also re-runs, which is surprising. Fix both with the
same mechanism.

## Approach

Add a persistent "already handled" marker in Redis, checked before enqueueing.
Use one marker per trigger source:

- Issue-body trigger for issue N → key `aizu:seen:issue:<repo>#<N>`
- Comment trigger for comment C → key `aizu:seen:comment:<repo>#<C>`

Semantics: set the marker when the task is **enqueued** (not when it
completes), with a TTL of 30 days (long enough to outlive any realistic issue
activity; keeps Redis from accumulating keys forever). If the marker exists,
skip. This means:

- An issue body triggers **exactly once**, even across edits and bot replies.
  (To re-run, the user comments `aizu …` — the normal path.)
- A comment triggers exactly once; editing it does not re-run. Posting a new
  comment is the way to re-run. Document this in `docs/content/docs/` if the
  docs mention retriggering.

### Steps

1. In `internal/poller/poller.go`, add:

   ```go
   const seenIssuePrefix = "aizu:seen:issue:"
   const seenCommentPrefix = "aizu:seen:comment:"
   const seenTTL = 30 * 24 * time.Hour

   // markSeen records key if unseen; reports whether it was already seen.
   func (p *Poller) alreadySeen(ctx context.Context, key string) bool {
       ok, err := p.rdb.SetNX(ctx, key, "1", seenTTL).Result()
       if err != nil {
           slog.Warn("Could not check seen-marker; proceeding", "key", key, "error", err)
           return false // fail open: worst case is a duplicate run, not a dropped task
       }
       return !ok
   }
   ```

   `SETNX` makes check-and-mark atomic, so this is also safe if plan
   `feat-worker-concurrency` lands.

2. In `pollIssues`, before `p.q.Enqueue(...)`:

   ```go
   if p.alreadySeen(ctx, fmt.Sprintf("%s%s#%d", seenIssuePrefix, repo, issue.Number)) {
       continue
   }
   ```

3. In `pollRepo`'s comment loop, before `p.q.Enqueue(...)`:

   ```go
   if p.alreadySeen(ctx, fmt.Sprintf("%s%s#%d", seenCommentPrefix, repo, c.ID)) {
       continue
   }
   ```

4. Marker-vs-enqueue ordering: if `Enqueue` fails after the marker is set, the
   trigger is lost. Keep it simple: on `Enqueue` error, delete the marker
   (`p.rdb.Del(ctx, key)`) so the next poll retries. Note `Enqueue` returning
   `(nil, nil)` (skipped because already active) is **not** an error — keep the
   marker in that case.

5. Cursor churn: also change `saveIssuesSince` callers to store
   `latest.Add(time.Second)` so the inclusive `since` doesn't refetch the same
   newest issue every poll. This is an efficiency fix only — correctness now
   comes from the seen-markers. (GitHub timestamps have second granularity.)

6. Add `fmt` to imports if not present.

## Files to modify

- `internal/poller/poller.go` — all changes above.
- `internal/poller/poller_test.go` — new tests.

## Tests

`internal/poller/poller_test.go` currently tests `triggered`/`shouldTrigger`
with a bare `Poller` struct. The new behavior needs Redis; add
`github.com/alicebob/miniredis/v2` as a test dependency (`go get
github.com/alicebob/miniredis/v2`) and construct the poller with
`queue.New("redis://" + mr.Addr())`.

Cover, using a fake GitHub server (`github.NewWithBaseURL`, see
`e2e/e2e_test.go` for the pattern):

1. **Issue body triggers once**: two `pollOnce` calls where the fake server
   returns the same issue (bumped `updated_at` on the second call, simulating
   the bot's reply) → exactly one task in the queue (drain with
   `q.NextPending`), even after `MarkDone`.
2. **Edited comment does not re-trigger**: same comment ID returned in two
   polls with different `updated_at`/body → one task.
3. **Distinct comments both trigger**: two different comment IDs → two tasks.
4. **Enqueue-skip keeps the marker**: enqueue the same issue twice while the
   first task is still queued → second is skipped by the active-set, and after
   `MarkDone` a third poll still does not re-trigger.

## Verification

```sh
make build
go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual (optional): run Aizu against a scratch repo, create an issue with body
`aizu say hello`, and watch `docker compose logs -f aizu` — the agent must run
exactly once, and subsequent polls must log nothing for that issue.

## Out of scope

- Do not change the queue package or the enqueue Lua script.
- Do not add a config option for re-trigger policy.
- Do not switch the poller to webhooks or change poll intervals.
