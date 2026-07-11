# Feature: startup self-check — fail fast with actionable errors

**Branch:** `feat/startup-selfcheck`
**PR title:** `feat: validate config at startup and report misconfiguration clearly`

## Context

Aizu's pitch is "quick to set up" — which makes *misconfiguration feedback
part of the product*. Today the two most likely first-run mistakes produce
the two worst experiences:

- **Bad/expired token:** `main.go` calls `AuthenticatedUser`, logs a
  `Warn` ("self-comment filtering disabled"), and **keeps running**. Every
  subsequent API call fails; the user sees an endless stream of poll
  errors and no statement of the actual problem.
- **Typo'd repo, or bot not yet a collaborator:** the poller logs
  `Poll failed repo=... error=github: ... -> 404` **every 15 seconds,
  forever** (`internal/poller/poller.go:57`). A 404 from GitHub means
  either "doesn't exist" or "no access" — the user has to know that
  private repos 404 until the collaboration invite is accepted (the README
  documents this, but the logs never say it).

An intuitive tool checks its configuration once, up front, says exactly
what is wrong in the user's terms, and doesn't spam.

## Approach

Validate at startup: token first, then each watched repo. Fail hard when
nothing can work; explain precisely when something partially works. Then
throttle repeating poll errors.

### Steps

1. **Token check becomes fatal.** In `main.go`, the existing
   `AuthenticatedUser` call: on error, print an actionable message and
   exit instead of warning:

   ```
   Error: GitHub rejected the token (401). Check GITHUB_TOKEN in .env —
   it must be a classic personal access token with the `repo` scope
   (https://github.com/settings/tokens).
   ```

   Distinguish 401 (bad token) from network errors ("could not reach
   api.github.com: …") — the client's error string already contains the
   status code; match on `"-> 401"` or, better, have this check use a
   small typed error (see step 2's `CheckRepo` for the pattern; a
   `StatusError{Code int}` returned by `decode` in
   `internal/github/client.go` serves both). Keep running on pure network
   errors (GitHub might be down; that's not a config problem) — warn and
   continue only in that case.

2. **Repo check.** Add to `internal/github/client.go`:

   ```go
   // CheckRepo verifies the token can see the repo. A 404 means the repo
   // doesn't exist or the token has no access (GitHub does not distinguish).
   func (c *Client) CheckRepo(ctx context.Context, repoFull string) error {
       return c.get(ctx, fmt.Sprintf("%s/repos/%s", c.base(), repoFull), nil)
   }
   ```

   In `main.go` (poller modes only, after the token check), loop over
   `cfg.Repos`:

   - All repos OK → one line: `Watching 2 repos as <login>: owner/a, owner/b`.
   - A repo 404s → per-repo error with the real-world causes:

     ```
     Error: cannot access owner/repo (404). Either the name is misspelled,
     or this account lacks access. For private repos: add <login> as a
     collaborator AND accept the invite from the <login> account.
     ```

   - **All** repos inaccessible → exit 1. **Some** accessible → warn for
     the bad ones and continue with the good ones (drop the bad ones from
     `cfg.Repos` so the poller doesn't spam about them).

3. **Throttle repeating poll errors.** In `internal/poller/poller.go`,
   runtime errors can still start after boot (token revoked, repo made
   private). Keep the first `slog.Error` per repo, then suppress
   repeats: add `lastErrLog map[string]time.Time` to `Poller` (init in
   `New`), and in `pollOnce`'s error branch only log if 10 minutes have
   passed since that repo's last logged error; reset the entry on a
   successful poll (also log one `Info: "Polling recovered"` when a repo
   transitions error→ok, so the log tells a complete story).

4. **Keep it proportional.** This plan must not grow a "doctor" framework:
   no new config, no retries-with-backoff machinery, no health endpoints.
   Total new code should be well under 100 lines.

## Files to modify

- `main.go`
- `internal/github/client.go` (+ `client_test.go`)
- `internal/poller/poller.go` (+ `poller_test.go`)

## Tests

- `client_test.go`: `CheckRepo` on 200 → nil; on 404 → error carrying the
  status (via the typed error if step 1 adds it).
- `poller_test.go`: with a fake server returning 500 for a repo, two
  immediate `pollOnce` calls log the error once (capture via a test
  `slog.Handler` or observe the map's timestamps directly); after
  advancing the throttle window, it logs again; a successful poll resets.
- Startup logic in `main.go` is thin glue — verify manually (below) rather
  than restructuring main for testability.

## Verification

```sh
make build && go test -race ./...
```

Manual, the three first-run failure modes:

1. Garbage `GITHUB_TOKEN` → process exits immediately with the token
   message; no poll loop starts.
2. `AIZU_REPOS=owner/nope,owner/real` → startup names the bad repo with
   the collaborator hint, then `Watching 1 repo…`; no recurring 404 spam.
3. Correct config → single `Watching N repos as <login>` line and quiet
   logs.

## Out of scope

- No `aizu init` wizard, no interactive prompts.
- No Docker or Redis reachability checks (compose already sequences Redis;
  Docker failures surface clearly at first task).
- No model-server checks — `feat-ollama-autodetect.md` owns that warning.
