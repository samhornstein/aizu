# Aizu Improvement Plans

This directory contains self-contained implementation plans, one per feature branch.
Each plan assumes **no prior context**: it explains the problem, the exact approach,
the files to touch, the tests to write, and how to verify. Execute each plan on its
own branch and open a PR against `main`.

**Implementation order, phases, and dependencies live in [ROADMAP.md](ROADMAP.md) —
read that first.**

PR titles must follow Conventional Commits (enforced by `.github/workflows/pr-title.yml`).

## Executing a plan

Plans are executed **sequentially, one at a time, in ROADMAP order** — not as
parallel branches. Later plans assume earlier plans' changes are already in
`main` (they patch each other's final shapes), so parallel branches produce
semantic conflicts that merge cleanly and break silently. Do not fan out.

Per plan:

1. Start from up-to-date `main` (the previous plan already merged). Create
   the branch named in the plan's header.
2. Implement exactly what the plan says. Its **Out of scope** section is a
   hard boundary — if the implementation seems to require exceeding it, stop
   and ask rather than improvise. Follow the philosophy guardrails at the
   top of [ROADMAP.md](ROADMAP.md).
3. Run the plan's **Verification** section. Do not open a PR until it passes.
4. In the same PR: delete the plan file and remove its row from the index
   below, so this directory always lists exactly the remaining work.
5. Open the PR with the title from the plan's header; let CI pass; a human
   reviews and merges before the next plan begins.

Suggested session prompt: *"Read plans/ROADMAP.md, then implement
plans/&lt;file&gt;.md. Follow it exactly, respect its Out of scope section, run
its Verification commands before opening a PR, and delete the plan file +
its README index row in the same PR."*

The only pair safe to run in parallel is `bugfix-stale-running-set.md` and
`bugfix-timeout-detection.md` (disjoint packages); it is rarely worth it.

## Index

| Plan | Category | Severity / Value | Branch |
|------|----------|------------------|--------|
| [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Bug | Medium — PRs from forks fail at checkout | `fix/fork-pr-checkout` |
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
