# Feature: single-account mode — no bot account required

**Branch:** `feat/single-account-mode`
**PR title:** `feat: identify Aizu replies by marker so a personal token works`

## Context

Aizu polls GitHub for comments/issues starting with a trigger keyword
(default `aizu`) and posts agent results back as comments. Today the
quickstart's **step 1 is "create a second GitHub bot account"** — the
single biggest piece of setup friction, and a real adoption killer for a
tool whose pitch is "low friction".

The bot account exists for one mechanical reason: the poller must not react
to Aizu's own replies, and it currently distinguishes them **by author** —
`main.go` resolves the token's login into `cfg.BotUsername`, and
`internal/poller/poller.go` skips comments (`shouldTrigger`) and issues
(`pollIssues`) authored by that account. If a user ran Aizu with their
*personal* PAT, `BotUsername` would equal their own login and **their own
trigger comments would be ignored** — hence the second account.

**The fix:** identify Aizu's replies by *content*, not author. The worker
appends an invisible HTML marker to every comment it posts; the poller
skips anything containing the marker. Then a personal PAT works, and the
quickstart shrinks to "paste a token you already have".

## Approach

### Steps

1. **Define the marker.** In `internal/github/client.go` (it's the shared
   package both worker and poller import):

   ```go
   // ReplyMarker is appended (invisibly) to every comment Aizu posts, so the
   // poller can recognize and skip Aizu's own output regardless of which
   // account posted it. This is what makes running Aizu with a personal
   // token possible.
   const ReplyMarker = "<!-- aizu-reply -->"
   ```

2. **Stamp outgoing comments.** In `Client.CreateComment`, append the
   marker to every body:

   ```go
   body = strings.TrimRight(body, "\n") + "\n\n" + ReplyMarker
   ```

   Doing it in the client (not the worker) guarantees no call site forgets.
   HTML comments render as nothing on GitHub.

3. **Filter by marker in the poller.** In `internal/poller/poller.go`:
   - In `shouldTrigger`, add before the trigger check:

     ```go
     if strings.Contains(c.Body, github.ReplyMarker) {
         return false // one of our own replies
     }
     ```

   - In `pollIssues`, similarly skip issues whose body contains the marker.
   - **Keep** the existing `BotUsername` checks as a secondary filter — they
     are still correct when a dedicated bot account *is* used, and harmless
     otherwise. But they must no longer be load-bearing.

4. **Fix the self-lockout for personal tokens.** The `BotUsername` filter
   as written blocks the user's own comments when the token is personal.
   Two options; take the simple one: only apply the author filter when the
   authenticated account is *not* expected to be the triggering user — i.e.
   drop the author filter entirely and rely on the marker. Concretely:
   remove the `c.User.Login == p.cfg.BotUsername` check from
   `shouldTrigger` and the equivalent in `pollIssues`. The marker (plus the
   seen-markers from `bugfix-issue-retrigger-loop.md`, which should land
   first) now prevents self-reaction in both modes. Keep `BotUsername`
   resolution in `main.go` for logging ("Authenticated as X"), and keep the
   field in config (other code may use it later).

5. **Edge cases to note in code comments:**
   - Agent output that itself begins with the trigger word: covered — the
     posted reply carries the marker.
   - Comments the *agent* posts via `gh` (after `feat-sandbox-github-api.md`):
     these lack the marker, but they don't begin with the trigger keyword
     (progress checklists start with `- [ ]`), so they don't trigger. Add
     one line to `AIZU.md`: "Never begin a comment, issue, or PR body with
     the word `aizu`."
   - Reactions and comments now come *from the user's own account* in
     single-account mode — cosmetic; mention it in docs.

6. **Rewrite the quickstart.** In `README.md` (and the mirrored
   `docs/content/docs/getting-started/_index.md`):
   - New step 1: create a classic PAT with `repo` scope on **your own
     account**.
   - Move the entire bot-account section to an optional
     "Give Aizu its own identity" subsection (kept for people who want
     replies attributed to a separate account).
   - Update `.env.example`'s GITHUB_TOKEN comment accordingly.

## Files to modify

- `internal/github/client.go`
- `internal/poller/poller.go`
- `main.go` (only if the BotUsername warning text needs rewording)
- `AIZU.md`
- `README.md`, `docs/content/docs/getting-started/_index.md`, `.env.example`
- `internal/github/client_test.go`, `internal/poller/poller_test.go`,
  `e2e/e2e_test.go`

## Tests

1. `client_test.go`: `CreateComment` body received by the fake server ends
   with `ReplyMarker`.
2. `poller_test.go`: a comment containing the marker does not trigger even
   when it starts with the trigger keyword; an issue body with the marker
   does not trigger.
3. **The headline case** — poller and worker share one identity: extend
   `e2e/e2e_test.go` (or add a poller test) where the triggering comment's
   author equals `cfg.BotUsername`; the task must still run (it would not,
   before this change), and a second poll returning Aizu's own
   marker-stamped reply must not enqueue anything.

## Verification

```sh
make build && go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: put a **personal** PAT in `.env` (no bot account), comment
`aizu hello` on a test issue from that same account → the agent runs and
replies; the reply is not re-triggered on subsequent polls.

## Out of scope

- Do not remove bot-account support or `BotUsername` from config.
- Do not change reactions, queue, or executor.
- No migration for pre-existing unmarked replies in old threads (they only
  matter if they start with the trigger word — vanishingly rare; the
  seen-marker dedupe covers repeats anyway).
