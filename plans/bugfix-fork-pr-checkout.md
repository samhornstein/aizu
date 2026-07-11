# Fix: pull requests from forks fail at checkout

**Branch:** `fix/fork-pr-checkout`
**PR title:** `fix: check out fork PRs via pull/N/head ref`

## Context

When a trigger fires on a pull request, the worker
(`internal/worker/worker.go` `handle()`) fetches the PR to get its head
branch name (`pr.Head.Ref`) and passes it to the executor. The executor
(`internal/executor/container.go` `Create()`) clones the **base** repo and
runs `git checkout <branch>`.

**The bug:** if the PR comes from a fork, its head branch exists only in the
fork, not in the base repo — the checkout fails with
`git checkout <branch>: exit status 1` and the user gets a failure comment.
Same-repo PRs work only by luck of the branch existing in the base clone.

GitHub exposes every PR's head (fork or not) in the base repo under the ref
`refs/pull/<number>/head`, readable with the same token used for the clone.

A second consequence for forks: even after checkout is fixed, **push won't
work** — the bot token has no write access to the contributor's fork. The
agent must be told this so it doesn't fail confusingly, and the AIZU.md
instruction "push to the existing PR branch" must be qualified.

## Approach

Fetch `pull/<n>/head` into a local branch instead of checking out by name,
and tell the agent when it cannot push.

### Steps

1. **Thread the PR number and fork flag through.** In
   `internal/github/client.go`, extend `PullRequest.Head` with the source
   repo so the worker can detect forks:

   ```go
   type PullRequest struct {
       Number int `json:"number"`
       Head   struct {
           Ref  string `json:"ref"`
           Repo struct {
               FullName string `json:"full_name"`
           } `json:"repo"`
       } `json:"head"`
   }
   ```

   (GitHub's `head.repo` can be `null` when a fork was deleted; with struct
   nesting as above it decodes to zero values — treat empty `FullName` as a
   fork/unpushable case.)

2. **Change the Executor contract.** In `internal/executor/executor.go`,
   change `Create(repo, branch string)` to
   `Create(repo, branch string, prNumber int)` — `prNumber` is 0 for
   non-PR (issue) tasks. Update the interface doc comment. Update all
   implementations and call sites:
   - `internal/executor/container.go`
   - `internal/worker/worker.go` (`w.exec.Create(task.Repo, branch)` →
     pass `issue.Number` when `issue.IsPR()`, else 0)
   - `e2e/e2e_test.go` mockExec
   - any mock in `internal/worker/worker_test.go`

3. **Fetch the PR ref in `Create`.** In `container.go`, replace the
   branch-checkout block:

   ```go
   if prNumber > 0 {
       fetch := fmt.Sprintf("cd /workspace/repo && git fetch origin %s && git checkout -B %s FETCH_HEAD",
           shellQuote(fmt.Sprintf("pull/%d/head", prNumber)),
           shellQuote(branch))
       if _, err := e.exec(sid, fetch, 0); err != nil {
           return "", fmt.Errorf("fetch PR #%d: %w", prNumber, err)
       }
   } else if branch != "" {
       // existing checkout path (kept for non-PR callers that pass a branch)
   }
   ```

   Note: cloning then fetching `pull/N/head` works for both same-repo and
   fork PRs, so use it unconditionally for PRs. Do **not** use the
   `pull/N/head:<branch>` refspec form — git refuses to fetch into the
   currently checked-out branch, so it breaks whenever `pr.Head.Ref` equals
   the clone's default branch (e.g. a fork PR opened from the fork's
   `main`). `git checkout -B <branch> FETCH_HEAD` has no such restriction
   (it resets the branch even if current). The local branch is named after
   `pr.Head.Ref` so `git push origin <branch>` still does the right thing
   for same-repo PRs.

4. **Tell the agent when it can't push.** In `internal/worker/worker.go`:
   - In `handle()`, after fetching the PR, compute
     `isFork := pr.Head.Repo.FullName == "" || pr.Head.Repo.FullName != task.Repo`.
   - Pass it into `buildPrompt` (add a parameter or a small struct).
   - In `buildPrompt`, when responding on a fork PR, append:

     ```
     Note: this pull request comes from a fork. You cannot push to its
     branch. Do not attempt to push; instead, describe the changes you would
     make, or include a patch in your reply.
     ```

5. **Qualify AIZU.md.** In the repo-root `AIZU.md` rules section, change the
   PR rule to: "If you are responding on a **pull request**, commit and push
   your changes to the existing PR branch. (If the prompt notes the PR comes
   from a fork, do not push — reply with your findings or a patch instead.)"

## Files to modify

- `internal/github/client.go`
- `internal/executor/executor.go`
- `internal/executor/container.go`
- `internal/worker/worker.go`
- `internal/worker/worker_test.go`
- `e2e/e2e_test.go` (mockExec signature)
- `AIZU.md`

## Tests

- `internal/worker/worker_test.go`: extend the fake executor to record the
  `prNumber` it received; add a case where the fake GitHub server returns a
  PR whose `head.repo.full_name` differs from the task repo and assert the
  built prompt contains the fork note; assert same-repo PRs do not get the
  note and that `prNumber` is threaded through.
- `internal/github/client_test.go`: decoding test for `head.repo.full_name`
  including the `"repo": null` case.

## Verification

```sh
make build
go test -race ./...
docker run --rm -d -p 6379:6379 --name aizu-test-redis redis:7-alpine
make test-e2e
docker rm -f aizu-test-redis
```

Manual: on a watched repo, open a PR **from a fork** (or ask a friend to),
comment `aizu review this` — before the fix this fails with a checkout
error; after, the agent runs against the PR's actual code and replies
without attempting to push.

## Out of scope

- Do not implement pushing to forks (`maintainer can edit` tokens, etc.).
- Do not change clone strategy (full clone stays; no shallow-clone work).
- Do not touch the poller or queue.
