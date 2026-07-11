# Aizu Improvement Plans

This directory contains self-contained implementation plans, one per feature branch.
Each plan assumes **no prior context**: it explains the problem, the exact approach,
the files to touch, the tests to write, and how to verify. Execute each plan on its
own branch and open a PR against `main`.

**Implementation order, phases, and dependencies live in [ROADMAP.md](ROADMAP.md) —
read that first.**

PR titles must follow Conventional Commits (enforced by `.github/workflows/pr-title.yml`).

## Index

| Plan | Category | Severity / Value | Branch |
|------|----------|------------------|--------|
| [bugfix-issue-retrigger-loop.md](bugfix-issue-retrigger-loop.md) | Bug | **Critical** — issue-body triggers re-run the agent forever | `fix/issue-retrigger-loop` |
| [bugfix-stale-running-set.md](bugfix-stale-running-set.md) | Bug | **High** — a crash mid-task permanently blocks an issue | `fix/stale-running-set` |
| [bugfix-timeout-detection.md](bugfix-timeout-detection.md) | Bug | Medium — engine timeouts are misreported as generic failures | `fix/timeout-detection` |
| [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Bug | Medium — PRs from forks fail at checkout | `fix/fork-pr-checkout` |
| [feat-single-account-mode.md](feat-single-account-mode.md) | Quickstart | **High** — removes the "create a bot account" setup step | `feat/single-account-mode` |
| [feat-ollama-autodetect.md](feat-ollama-autodetect.md) | Quickstart | High — Ollama support + zero-config local model discovery | `feat/local-model-autodetect` |
| [feat-startup-selfcheck.md](feat-startup-selfcheck.md) | Quickstart | High — bad token/repo fails fast with instructions instead of log spam | `feat/startup-selfcheck` |
| [feat-prebuilt-images.md](feat-prebuilt-images.md) | Quickstart | High — install via one compose file, no clone/build | `feat/prebuilt-images` |
| [security-collaborator-gate.md](security-collaborator-gate.md) | Security | **High** — today anyone who can comment can trigger the agent | `fix/collaborator-gate` |
| [feat-sandbox-github-api.md](feat-sandbox-github-api.md) | Agent capability | High — makes AIZU.md's progress updates actually possible | `feat/sandbox-github-api` |
| [security-secrets-hygiene.md](security-secrets-hygiene.md) | Security | High — secrets visible in host `ps`; token could leak into public comments | `fix/secrets-hygiene` |
| [feat-feedback-polish.md](feat-feedback-polish.md) | UX | Medium — status reactions, `aizu help`, progress comment, rate limit | `feat/feedback-polish` |
| [feat-engine-presets.md](feat-engine-presets.md) | Feature | Medium — `AIZU_ENGINE=claude\|aider\|opencode` presets | `feat/engine-presets` |
| [feat-worker-concurrency.md](feat-worker-concurrency.md) | Feature | Medium — run multiple agent tasks in parallel | `feat/worker-concurrency` |
| [chore-code-quality.md](chore-code-quality.md) | Quality | Low — small cleanups and missing queue tests | `chore/code-quality` |

## Future ideas (not planned in detail)

See also the "Explicitly deferred" section of [ROADMAP.md](ROADMAP.md).

- **Webhook mode** — replace polling with a GitHub webhook receiver (needs a
  public endpoint or a tunnel; polling stays as the zero-setup default).
- **GitHub App auth** — replace the PAT setup with a GitHub App
  (short-lived installation tokens, finer permissions).
- **Sandbox network isolation** — run agent containers with a restricted
  network (e.g. an egress allowlist) instead of full network access.
- **`aizu init` setup wizard** — interactive `.env` generation with
  self-tests (token valid? model reachable? Docker working?).
