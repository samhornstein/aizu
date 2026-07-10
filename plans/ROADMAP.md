# Roadmap: implementation order

The product goal shapes this order: **a low-friction way for an individual
developer to talk to a locally running LLM through GitHub** — private
projects first, enterprise repos later — with a quickstart minimal enough
to attract users. So: stop the bleeding first, then cut setup friction,
then make it safe to show strangers, then widen and polish.

Work top to bottom. Within a phase, the listed order avoids merge
conflicts; plans in *different* phases should not be reordered across the
phase boundary without checking the dependency notes.

## Phase 0 — Stabilize (do before anything else)

| Order | Plan | Why here |
|-------|------|----------|
| 1 | [bugfix-issue-retrigger-loop.md](bugfix-issue-retrigger-loop.md) | Critical: issue-body triggers loop forever, burning tokens and spamming threads. Also introduces the Redis seen-markers that phase 1's single-account mode leans on. |
| 2 | [bugfix-stale-running-set.md](bugfix-stale-running-set.md) | A crash silently bricks an issue forever — the kind of bug that makes an early adopter quit. Creates `internal/queue/queue_test.go` used by later plans. |
| 3 | [bugfix-timeout-detection.md](bugfix-timeout-detection.md) | Small; rewrites `run()` in helpers.go, which the secrets plan builds on — land it first so that plan patches the final version. |
| 4 | [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Fork PRs fail today. Touches the `Executor` interface and `buildPrompt`; finishing it before phase 2/3 keeps those signatures stable. |

## Phase 1 — Quickstart friction (the adoption phase)

| Order | Plan | Why here |
|-------|------|----------|
| 5 | [feat-single-account-mode.md](feat-single-account-mode.md) | Deletes the "create a second GitHub account" quickstart step — the single biggest friction. Requires plan 1's seen-markers to be in place. |
| 6 | [feat-ollama-autodetect.md](feat-ollama-autodetect.md) | Deletes the `OPENAI_BASE_URL` quickstart step for most users and meets the local-LLM audience on Ollama. Independent of plan 5. |
| 7 | [feat-prebuilt-images.md](feat-prebuilt-images.md) | Deletes the clone-and-build quickstart steps. Goes last in the phase so the published image and rewritten README already contain plans 5–6. Cut a release after this and re-test the quickstart from scratch. |

After phase 1 the quickstart is: `curl` one compose file, paste a personal
PAT and a repo name into `.env`, `docker compose up -d`. That is the state
worth demoing.

## Phase 2 — Safe to share (the launch gate)

Do **all of phase 2 before any public sharing** (HN, company-wide, public
repos). Phases 0–1 make it good; phase 2 makes it safe.

| Order | Plan | Why here |
|-------|------|----------|
| 8 | [security-collaborator-gate.md](security-collaborator-gate.md) | Today anyone who can comment can run an agent on your machine. Must precede pointing Aizu at any public repo. |
| 9 | [feat-sandbox-github-api.md](feat-sandbox-github-api.md) | Makes AIZU.md's promised progress updates real. Ordered before the secrets plan because it adds the `GITHUB_TOKEN` export that plan then secures. |
| 10 | [security-secrets-hygiene.md](security-secrets-hygiene.md) | Keeps tokens out of host `ps` and — critically — redacts secrets from anything posted to a public thread. Builds on plans 3 and 9. |

## Phase 3 — Widen and polish

| Order | Plan | Why here |
|-------|------|----------|
| 11 | [feat-feedback-polish.md](feat-feedback-polish.md) | Completion reactions, `aizu help`, worker progress comment, rate limit. The rate limit is also the backstop for the plan-1 bug class — nice to have before heavy sharing. Changes `CreateComment`'s signature; do before plan 12/13 touch nearby code. |
| 12 | [feat-engine-presets.md](feat-engine-presets.md) | `AIZU_ENGINE=claude\|aider\|opencode` widens the funnel beyond pi. Coordinate with plan 7: presets should reference the published ghcr images, and the release workflow's build matrix grows per engine. |
| 13 | [feat-worker-concurrency.md](feat-worker-concurrency.md) | Matters once several people share one instance — a phase-3 concern by definition. Depends on plan 2's single-process assumption (documented there). |
| 14 | [chore-code-quality.md](chore-code-quality.md) | Anytime filler between larger plans; each of its four items is independent. If executed late, skip items already done in passing (its own header says how). |

## Cross-cutting dependency notes

- **Poller file contention:** plans 1, 5, and 8 all edit
  `internal/poller/poller.go`. Do them strictly sequentially (they are
  ordered that way above); rebase between.
- **Worker/client contention:** plans 4, 9, 10, and 11 all touch
  `internal/worker/worker.go` and/or `internal/github/client.go` —
  sequential, in the listed order.
- **`envExports` / helpers.go:** touched by plans 3, 9, 10, and 12 — the
  listed order applies each change to the previous plan's final shape.
- **README/quickstart:** plans 5, 6, 7, 8, and 12 each edit the quickstart.
  Conflicts are trivial (docs), but re-read the full quickstart after plan
  7 and again after plan 12 for coherence — plans edit their own section
  and can leave seams.
- **miniredis:** first introduced by whichever of plans 1/2 lands first;
  later plans reuse it. It is a test-only dependency.

## Launch checklist (after phase 2)

- [ ] Fresh-machine quickstart run-through, timed — target under 5 minutes
      from `curl` to first agent reply.
- [ ] Trigger from a non-collaborator account on a public repo → denied.
- [ ] `grep` a full task's logs and the GitHub thread for the token — absent.
- [ ] Kill the process mid-task, restart, re-trigger → works.
- [ ] README demo GIF/asciinema of a real issue→PR round trip (the docs
      site already ships asciinema assets — use them).

## Explicitly deferred (revisit only after real users exist)

- **Webhook mode** — polling behind NAT with zero inbound networking *is
  the product*; don't trade it for webhook setup friction.
- **GitHub App auth** — more setup, not less, for individuals. Revisit when
  an enterprise actually asks.
- **Hosted/multi-tenant anything** — the moat is "your machine, your
  models".
- **`aizu init` setup wizard** — attractive, but phases 1's changes may
  shrink `.env` to two lines, at which point a wizard is overkill. Re-judge
  after phase 1.
