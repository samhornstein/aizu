# Aizu Improvement Plans

This directory contains self-contained implementation plans, one per feature branch.
Each plan assumes **no prior context**: it explains the problem, the exact approach,
the files to touch, the tests to write, and how to verify. Execute each plan on its
own branch and open a PR against `main`.

PR titles must follow Conventional Commits (enforced by `.github/workflows/pr-title.yml`).

## Index

| # | Plan | Category | Severity / Value | Branch |
|---|------|----------|------------------|--------|
| 1 | [bugfix-issue-retrigger-loop.md](bugfix-issue-retrigger-loop.md) | Bug | **Critical** — issue-body triggers re-run the agent forever | `fix/issue-retrigger-loop` |
| 2 | [bugfix-stale-running-set.md](bugfix-stale-running-set.md) | Bug | **High** — a crash mid-task permanently blocks an issue | `fix/stale-running-set` |
| 3 | [bugfix-timeout-detection.md](bugfix-timeout-detection.md) | Bug | Medium — engine timeouts are misreported as generic failures | `fix/timeout-detection` |
| 4 | [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Bug | Medium — PRs from forks fail at checkout | `fix/fork-pr-checkout` |
| 5 | [feat-sandbox-github-api.md](feat-sandbox-github-api.md) | Agent capability | High — makes AIZU.md's progress updates actually possible | `feat/sandbox-github-api` |
| 6 | [security-secrets-hygiene.md](security-secrets-hygiene.md) | Security | High — secrets visible in host `ps`; token could leak into public comments | `fix/secrets-hygiene` |
| 7 | [chore-code-quality.md](chore-code-quality.md) | Quality | Low — small cleanups and missing queue tests | `chore/code-quality` |
| 8 | [feat-worker-concurrency.md](feat-worker-concurrency.md) | Feature | Medium — run multiple agent tasks in parallel | `feat/worker-concurrency` |

## Recommended order

**1 → 2 → 3 → 4 → 5 → 6 → 7 → 8.**

Plan 1 is the most damaging bug — do it first. Plan 5 before publicizing the
progress-update instructions any further. Plans 7 and 8 are independent
nice-to-haves.

## Dependencies between plans

- **2 ↔ 8**: plan 2's "clear the running set on startup" is written for a
  single process; plan 8 keeps all workers in one process, so it stays valid.
  Plan 8 notes this explicitly.
- **4 and 5** both touch the worker's prompt assembly (`buildPrompt`) and the
  `Executor` interface; do them sequentially, not in parallel, and rebase.
- Everything else is independent.

## Future ideas (not planned in detail)

- **Webhook mode** — replace polling with a GitHub webhook receiver (needs a
  public endpoint or a tunnel; polling stays as the zero-setup default).
- **GitHub App auth** — replace the PAT/bot-account setup with a GitHub App
  (short-lived installation tokens, finer permissions, no second account).
- **Streaming progress** — worker posts/edits a single "working…" comment while
  the engine runs, rather than relying on the agent to do it.
- **Sandbox network isolation** — run agent containers with a restricted
  network (e.g. an egress allowlist) instead of full host network access.
