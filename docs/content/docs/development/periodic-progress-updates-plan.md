# Plan: Periodic Progress Updates

> Goal: Replace the current "eyes" reaction + final-comment pattern with a single
> progress comment that is edited in-place as the agent works, similar to how
> dependabot updates PR descriptions.

## Current Flow

```
Poller detects @aizu comment
  → enqueues task to Redis
    → Worker picks task
      → adds "eyes" reaction to triggering comment
      → creates Docker sandbox (clone + checkout)
      → runs engine (pi) synchronously — blocks until done
      → posts reply comment with full result
      → marks task done
```

**Problem**: The user sees an "eyes" reaction and then silence until the task
completely finishes (potentially 10 minutes).

## Target Flow

```
Worker picks task
  → creates a "progress comment" on the issue/PR
  → creates Docker sandbox
  → runs engine with output streaming to a log file
  → background goroutine: every N seconds, read log file → edit progress comment
  → engine completes
  → final edit to progress comment with full result
  → marks task done
```

## Architecture Changes

### 1. GitHub Client — `internal/github/client.go`

Add an `EditComment` method to update an existing comment in-place.

```go
// EditComment updates the body of an existing issue/PR comment.
func (c *Client) EditComment(ctx context.Context, repoFull string, commentID int64, body string) error {
    u := fmt.Sprintf("%s/repos/%s/issues/comments/%d", apiBase, repoFull, commentID)
    return c.put(ctx, u, map[string]string{"body": body}, nil)
}
```

Also needs a `put` helper (PATCH request) — the existing `post` helper can be
adapted or a new one added. The GitHub API endpoint is:

```
PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}
```

The `CreateComment` method already returns a `Comment` object (currently
discarded) — we need to capture the `ID` of the created progress comment so we
can edit it later. This means `CreateComment` should return the comment ID, or
we add a `CreateCommentAndReturnID` variant.

**Changes:**
- Add `EditComment(ctx, repoFull, commentID, body)` — uses PATCH
- Modify `CreateComment` to return the created comment's `ID` (int64), or add
  a new method. The GitHub API returns the full comment object on creation;
  we just need to decode the `id` field.
- Add `put` HTTP helper (PATCH method) alongside existing `post` (POST) and
  `get` (GET).

### 2. Executor — `internal/executor/`

The engine currently runs synchronously via `RunEngine`, which returns only
after the full command completes. We need incremental access to engine output.

**Option A — File-based polling (simplest, recommended):**

Modify the engine command to write output to a known file path, and add a
method to read partial output.

```go
const engineLogFile = "/tmp/.aizu-engine-log"

// In RunEngine (or a new RunEngineWithLog variant):
// Append " | tee -a /tmp/.aizu-engine-log" to the engine command so output
// goes to both stdout (for the final result) and a log file (for progress).
// Use stdbuf -oL to force line-buffered output:
//   stdbuf -oL pi -p "$(cat {prompt_file})" | tee -a /tmp/.aizu-engine-log
```

The existing `ReadFile(sid, path)` method already provides container file
access. No new executor interface methods are needed.

**Option B — Streaming callback (cleaner but more invasive):**

Add a callback parameter to `RunEngine`:

```go
type ProgressFn func(partialOutput string)

RunEngine(sid, prompt string, progress ProgressFn) (exitCode int, output string, err error)
```

This requires rewriting the container execution to use `docker logs -f` or
capture stdout in real-time. More work but cleaner separation.

**Recommendation: Option A** — minimal changes, uses existing `ReadFile`, and
avoids restructuring the executor interface.

### 3. Worker — `internal/worker/worker.go`

The worker's `process` method needs to:

1. **Create a progress comment** at the start (instead of just adding a reaction):

```go
commentID, err := w.gh.CreateCommentWithID(ctx, task.Repo, task.Number,
    "⏳ Aizu is working on this...")
```

2. **Run the engine** with output going to a log file (via executor change).

3. **Start a progress-updater goroutine** that polls the log file and edits
   the comment on a timer:

```go
ticker := time.NewTicker(30 * time.Second)
done := make(chan struct{})
go func() {
    for {
        select {
        case <-done:
            return
        case <-ticker.C:
            partial, _ := w.exec.ReadFile(sid, engineLogFile)
            if partial != "" {
                w.gh.EditComment(ctx, task.Repo, commentID,
                    formatProgress(partial))
            }
        }
    }
}()
defer func() { ticker.Stop(); close(done) }()
```

4. **On completion**, do a final `EditComment` with the full result.

5. **Remove the "eyes" reaction** (or keep it as a nice touch — either way).

**Key design decisions:**
- **Update interval**: 30 seconds is a reasonable default. Too frequent and we
  hit GitHub API rate limits; too infrequent and it feels stale.
- **Rate limiting**: GitHub allows 5,000 requests/hour for authenticated users.
  At 30s intervals per task, that's 120 updates/hour/worker — well within limits.
  With multiple concurrent tasks, we should add a simple rate limiter (e.g.,
  token bucket or semaphore) to stay under ~500/hour as a safety margin.
- **Truncation**: Each progress update should truncate the log to a reasonable
  size (e.g., last 6,000 characters) to stay within GitHub's comment size limits
  and avoid unnecessary API payload.
- **Comment marker**: The progress comment should have a unique marker (e.g.,
  `<!-- aizu-progress -->`) so that if the feature is later enhanced to detect
  existing progress comments, it can edit rather than create a new one.

### 4. Configuration — `internal/config/config.go`

Add optional config for the progress update behavior:

```go
// Config additions:
ProgressUpdateInterval int  // seconds between progress updates (default: 30, 0 = disabled)
ProgressEnabled        bool // whether progress updates are enabled (default: true)
```

Environment variables:
- `AIZU_PROGRESS_INTERVAL` — seconds between updates
- `AIZU_PROGRESS_ENABLED` — enable/disable (default: true)

### 5. Prompt/Instructions — `AIZU.md`

Update the system prompt to inform the agent that its output is being streamed
as progress updates:

```markdown
## Progress Updates

Your output is being streamed as progress updates to a GitHub comment. Write
concise status messages as you work. Your final output will be posted as the
final update.
```

This helps the agent produce useful incremental output rather than silence
followed by a large block of text.

## Implementation Order

1. **GitHub client**: `EditComment` + `CreateComment` returning ID + `put` helper
2. **Executor**: Modify engine command to tee output to log file
3. **Worker**: Progress comment creation + periodic update goroutine
4. **Config**: Add progress update settings
5. **Tests**: Unit tests for each component
6. **Prompt**: Update AIZU.md

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| GitHub API rate limits from frequent edits | Configurable interval, default 30s, rate limiter |
| Comment size limits (65,536 chars) | Truncate progress to ~6KB |
| Engine output buffering | Use `stdbuf -oL` for line buffering |
| Stale progress on failure | Final edit with error message before marking failed |
| Multiple triggers on same issue | Existing dedup logic prevents concurrent tasks; progress comment is per-task |

## Out of Scope (Future)

- **Detecting existing progress comment**: If a user triggers a second task on
  the same issue, create a new progress comment rather than editing the old one
  (avoids confusion). A future enhancement could detect and edit the existing
  one.
- **Progress comment on PR description**: The dependabot pattern edits the PR
  body itself. This is more complex (requires lock/unlock to avoid conflicts
  with pushes) and is not needed for the initial implementation.
- **Rich progress formatting**: Markdown tables, checklists, etc. Keep it simple
  initially — just the engine's output text.
