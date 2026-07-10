# Security: only collaborators with write access can trigger by default

**Branch:** `fix/collaborator-gate`
**PR title:** `fix: require repo write permission to trigger by default`

## Context

Aizu runs an agent **on the operator's own machine**, with the operator's
GitHub token available inside the sandbox, whenever a watched issue/PR gets
a comment starting with the trigger keyword. The only author gate today is
`AIZU_USERS` (`internal/config/config.go`), an optional manual allowlist —
and its documented default is **empty = allow everyone**
(poller checks: `if len(p.cfg.Users) > 0 && !contains(...)` in
`internal/poller/poller.go` `shouldTrigger` and `pollIssues`).

On a private repo that's fine. On a **public** repo it means any GitHub
account on the internet can execute an agent on your machine and spend your
tokens/compute — this must be fixed before the project is shared broadly.

**New default:** when `AIZU_USERS` is unset, only users with `write` or
`admin` permission on that repo may trigger. `AIZU_USERS` remains as an
explicit override (e.g. to allow a specific outside contributor). GitHub
exposes exactly this via
`GET /repos/{owner}/{repo}/collaborators/{username}/permission` →
`{"permission": "admin"|"write"|"read"|"none", ...}`.

## Approach

### Steps

1. **Client method.** In `internal/github/client.go`:

   ```go
   // Permission returns the named user's permission level on the repo:
   // "admin", "write", "read", or "none". Requires push access on the token
   // for private repos; on error the caller should fail closed.
   func (c *Client) Permission(ctx context.Context, repoFull, username string) (string, error) {
       var out struct {
           Permission string `json:"permission"`
       }
       u := fmt.Sprintf("%s/repos/%s/collaborators/%s/permission", c.base(), repoFull, url.PathEscape(username))
       if err := c.get(ctx, u, &out); err != nil {
           return "", err
       }
       return out.Permission, nil
   }
   ```

2. **Authorization helper in the poller.** In
   `internal/poller/poller.go`, replace the two inline `AIZU_USERS` checks
   (comment path in `shouldTrigger`, issue path in `pollIssues`) with one
   method used by both:

   ```go
   // authorized reports whether login may trigger Aizu on repo. With an
   // explicit AIZU_USERS allowlist, membership decides. Otherwise the user
   // must have write or admin permission on the repo. Errors fail closed.
   func (p *Poller) authorized(ctx context.Context, repo, login string) bool
   ```

   Logic:
   - `len(p.cfg.Users) > 0` → `contains(p.cfg.Users, login)` (unchanged
     behavior).
   - else consult a small in-memory cache (see step 3); on miss call
     `p.gh.Permission(ctx, repo, login)`; allow iff `admin` or `write`.
   - On API error: log Warn and **return false** (fail closed — an
     authorization check must not fail open).
   - On deny: keep the existing Info log style
     ("Ignoring comment from unauthorized user").

   Note `shouldTrigger` currently doesn't take a `ctx` — add it (callers
   have one). Order the checks so the (free) trigger-prefix check runs
   before the (API-costing) permission check.

3. **Cache.** One permission lookup per comment would add an API call per
   trigger — cheap — but polls also see repeated authors; cache to keep
   rate-limit use flat:

   ```go
   type permCacheEntry struct {
       allowed bool
       expires time.Time
   }
   // on Poller: perms map[string]permCacheEntry  (key: repo+"/"+login), plus a sync.Mutex
   ```

   TTL 10 minutes (revoking someone's access takes effect within that).
   Initialize the map in `New`. Mutex because `feat-worker-concurrency.md`
   doesn't touch the poller, but cheap insurance costs nothing.

4. **Escape hatch.** Some users will want the old behavior (e.g. a private
   repo where "everyone here is trusted" includes read-only members). Add
   `AIZU_ALLOW_ALL=true` (config field `AllowAll bool`, env parse alongside
   the others in `Load()`); when set, `authorized` returns true after the
   allowlist check. Document it with a warning in `.env.example`:

   ```
   # AIZU_ALLOW_ALL=true                        # DANGER: let anyone who can comment trigger the agent
   ```

5. **Docs.** README + `docs/content/docs/best-practices/_index.md`: state
   the default ("only people with write access can trigger"), how
   `AIZU_USERS` and `AIZU_ALLOW_ALL` modify it, and why this matters on
   public repos.

## Files to modify

- `internal/github/client.go` (+ `client_test.go`)
- `internal/poller/poller.go` (+ `poller_test.go`)
- `internal/config/config.go` (+ `config_test.go`)
- `.env.example`, `README.md`, `docs/content/docs/best-practices/_index.md`
- `e2e/e2e_test.go` — the fake server needs a
  `/repos/o/r/collaborators/{user}/permission` handler returning `write`
  (otherwise the pipeline test's trigger is now denied — this test breaking
  is itself a good check that the gate works)

## Tests

- `client_test.go`: `Permission` decodes each level; 404 → error.
- `poller_test.go` (fake server + miniredis, as in earlier plans):
  1. No allowlist: `write` author → enqueued; `read` author → not; `none`/
     404 → not.
  2. Permission endpoint returns 500 → not enqueued (fail closed).
  3. `AIZU_USERS=alice`: alice enqueued **without** any permission API call
     (assert the fake server endpoint was not hit); bob denied even with
     write.
  4. `AllowAll` → read-only author enqueued.
  5. Cache: two comments by the same author in one poll → exactly one
     permission request (count hits on the fake handler).

## Verification

```sh
make build && go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: on a public test repo, comment from a second account that has no
access → logs show the deny, no reaction/reply appears; grant that account
write → after ≤10 min (cache TTL) it can trigger.

## Out of scope

- No org-team allowlists or per-repo config.
- No changes to what the *agent* is allowed to do once triggered.
- Rate limiting is `feat-feedback-polish.md`'s job, not this plan's.
