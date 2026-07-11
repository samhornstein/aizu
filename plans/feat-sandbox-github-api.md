# Feature: give the agent sandbox GitHub API access

**Branch:** `feat/sandbox-github-api`
**PR title:** `feat: provide gh CLI and GITHUB_TOKEN in the agent sandbox`

## Context

Aizu runs the coding agent inside a throwaway Docker container built from
`templates/pi/Dockerfile`, with the target repo cloned at `/workspace/repo`.
The prompt prepended to every task (repo-root `AIZU.md`, embedded in the
binary and overridable per-repo via `.aizu/AIZU.md`) instructs the agent to:

- open pull requests when responding to issues, and
- post a progress-outline comment and keep editing it via "the GitHub API".

**The gap:** the sandbox cannot follow those instructions. The environment
passed to the engine (`envExports` in `internal/executor/helpers.go`)
contains only a git identity and model API keys. There is no `gh` CLI in the
agent image and no `GITHUB_TOKEN` in the environment. The only credential
present is the token embedded in the clone URL's remote (which is why
`git push` works) — so "open a pull request" and "update a comment" silently
can't happen, and agents either skip those steps or fail noisily.

Decision (made by the project owner): give the sandbox real API access
rather than removing the instructions. Security note: the token is *already*
readable by the agent from `.git/config` (remote URL), so exporting it as an
env var does not create a new exposure class.

## Approach

Install `gh` in the agent image, export `GITHUB_TOKEN`/`GH_TOKEN` into the
engine's environment, and rewrite AIZU.md's instructions around concrete
`gh` commands.

### Steps

1. **Install `gh` in `templates/pi/Dockerfile`** (Debian bookworm-slim
   base). Use GitHub's official apt repo:

   ```dockerfile
   RUN apt-get update \
     && apt-get install -y --no-install-recommends curl gnupg \
     && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
          -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
     && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
          > /etc/apt/sources.list.d/github-cli.list \
     && apt-get update \
     && apt-get install -y --no-install-recommends gh \
     && rm -rf /var/lib/apt/lists/*
   ```

   Merge with the existing apt-get layer if convenient; keep
   `--no-install-recommends` and the lists cleanup.

2. **Export the token.** In `internal/executor/helpers.go` `envExports`,
   add to the credentials map:

   ```go
   "GITHUB_TOKEN": cfg.GitHubToken,
   "GH_TOKEN":     cfg.GitHubToken,
   ```

   (`gh` reads `GH_TOKEN` first, then `GITHUB_TOKEN`; many other tools read
   `GITHUB_TOKEN` — export both.) `envExports` already receives the config;
   no signature change needed.

3. **Rewrite the AIZU.md guidance** (repo-root `AIZU.md`) so it only asks
   for things the sandbox can now do, with concrete commands. Replace the
   "Progress Updates" section with something like:

   ```markdown
   ## Tools

   The `gh` CLI is installed and authenticated (`GH_TOKEN`). Use it for all
   GitHub operations. You are inside a clone at /workspace/repo with push
   access via the `origin` remote.

   ## Progress Updates

   1. Before starting work, post a checklist comment:
      `gh issue comment <number> --repo <owner/repo> --body '- [ ] step…'`
   2. As you finish each step, edit that same comment (get its ID from the
      first command's output URL):
      `gh api -X PATCH repos/<owner/repo>/issues/comments/<id> -f body='…'`
   3. Do not post a new comment per step.
   ```

   Also update the Rules section: "open a pull request" can now cite
   `gh pr create --repo <owner/repo> --title … --body …`. Keep it concise —
   this whole file is prepended to every prompt for possibly-small local
   models; do not let it balloon. The worker's prompt already includes the
   repo and issue number (`buildPrompt` in `internal/worker/worker.go`), so
   the agent has the values to substitute.

4. **Docs.** `docs/content/docs/` mentions the agent's capabilities in
   places (getting-started / best-practices) — grep for "progress" and
   "pull request" and update statements that said the agent can't call the
   API, if any.

## Files to modify

- `templates/pi/Dockerfile`
- `internal/executor/helpers.go`
- `AIZU.md`
- `docs/content/docs/**` (only if statements there contradict the change)

## Tests

- `internal/executor/helpers_test.go` already tests `envExports`; add
  assertions that `GITHUB_TOKEN`/`GH_TOKEN` appear when
  `cfg.GitHubToken` is set and are absent when it is empty (the existing
  map-based loop already skips empty values — verify).
- Image build is covered by verification below (no unit test for
  Dockerfiles).

## Verification

```sh
make build
go test -race ./...
docker compose build agent
docker run --rm -e GH_TOKEN=<a-real-token> aizu-agent:pi gh api user -q .login
```

The last command must print the bot account's login. End-to-end: trigger
`aizu` on a test issue and confirm the agent posts a checklist comment,
edits it, and (for issues) opens a PR with `gh pr create`.

## Out of scope

- No scoping-down of the token (that's `security-secrets-hygiene.md` /
  future GitHub App work).
- Do not add other tools to the agent image.
- Do not change the worker's own commenting behavior (it still posts the
  final result comment itself).
