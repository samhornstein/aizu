# Roadmap: milestones and implementation order

The product goal shapes this order: **a low-friction way for an individual
developer to talk to a locally running LLM through GitHub** — private
projects first, enterprise repos later — with a quickstart minimal enough
to attract users.

Two milestones define the cut lines:

- **v0.1 — share with friends & coworkers.** Trusted users, mostly private
  or personal repos, willing to clone and build. The bar: *it must not
  misbehave* (no loops, no silently bricked issues) and the worst setup
  friction must be gone.
- **HN launch.** Strangers, public repos, and an audience that will read
  the code and probe the security model within the hour. The bar: *safe by
  default, installable in five minutes, and it must not embarrass you.*

Work top to bottom. Within a milestone, the listed order avoids merge
conflicts; don't reorder across milestone boundaries without checking the
dependency notes at the end.

## Milestone 1 — v0.1 (friends & coworkers)

| Order | Plan | Why it gates v0.1 |
|-------|------|-------------------|
| 1 | [bugfix-issue-retrigger-loop.md](bugfix-issue-retrigger-loop.md) | Critical: issue-body triggers loop forever, burning tokens and spamming threads — the fastest way to lose your first users. Also introduces the Redis seen-markers that plan 5 leans on. |
| 2 | [bugfix-stale-running-set.md](bugfix-stale-running-set.md) | A crash silently bricks an issue forever; "it just stopped responding" is unrecoverable trust damage with early users. Creates `internal/queue/queue_test.go` used by later plans. |
| 3 | [bugfix-timeout-detection.md](bugfix-timeout-detection.md) | The timeout path is dead code today — long runs surface as inscrutable generic failures. Small fix; also rewrites `run()`, which plan 10 builds on. |
| 4 | [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Coworkers are exactly who opens fork PRs. Touches the `Executor` interface and `buildPrompt`; finishing it now keeps those signatures stable for plans 9–11. |
| 5 | [feat-single-account-mode.md](feat-single-account-mode.md) | "Create a second GitHub account" is the step where a curious friend gives up. Requires plan 1's seen-markers. |
| 6 | [feat-ollama-autodetect.md](feat-ollama-autodetect.md) | Friends run Ollama, not llama.cpp; auto-detection deletes the `OPENAI_BASE_URL` step and the startup warning explains the most common misconfiguration. |

**v0.1 exit criteria** — then run the Release workflow and tag `v0.1.0`:

- [ ] All six plans merged; full quickstart re-tested from scratch on a
      clean checkout (clone + build is still the install path — fine for
      this audience).
- [ ] One coworker who is not you completes the quickstart with no help.
- [ ] README carries a temporary warning: **do not watch public repos yet**
      (anyone who can comment can trigger the agent until plan 8 lands).

*Optional if a spare hour appears:* the `aizu help` section (§2) of
[feat-feedback-polish.md](feat-feedback-polish.md) — it's independent,
tiny, and disproportionately useful for people just discovering the tool.

## Milestone 2 — HN launch

Do **all of these before any public sharing** (HN, company-wide, watching
public repos). Milestone 1 makes it good; this milestone makes it safe by
default and installable in minutes.

| Order | Plan | Why it gates the launch |
|-------|------|-------------------------|
| 7 | [security-collaborator-gate.md](security-collaborator-gate.md) | Today anyone who can comment can run an agent on your machine. The first question every HN commenter will ask; must be the default, not a setting. Removes the v0.1 README warning. |
| 8 | [feat-sandbox-github-api.md](feat-sandbox-github-api.md) | Makes AIZU.md's promised progress updates real (the demo depends on this looking alive). Ordered before plan 9 because it adds the `GITHUB_TOKEN` export that plan 9 then secures. |
| 9 | [security-secrets-hygiene.md](security-secrets-hygiene.md) | Keeps tokens out of host `ps` and — critically for public threads — redacts secrets from anything posted to GitHub. Builds on plans 3 and 8. |
| 10 | [feat-feedback-polish.md](feat-feedback-polish.md) | §4 (rate limit) is **required** — the backstop against runaway runs when strangers can see your bot. §1 (reactions), §2 (help), §3 (progress comment) are what make the demo GIF feel polished; do all four unless time-boxed, in which case §4 > §3 > §2 > §1. |
| 11 | [feat-prebuilt-images.md](feat-prebuilt-images.md) | The HN install must be: curl one compose file, two-line `.env`, `docker compose up`. Last before launch so the published image and rewritten README contain everything above. Cut a release after merging. |

**Launch checklist** — after plan 11's release:

- [ ] Fresh-machine quickstart run-through, timed — target under 5 minutes
      from `curl` to first agent reply.
- [ ] Trigger from a non-collaborator account on a public repo → denied,
      logged, no reply.
- [ ] `grep` a full task's logs, `ps` output during a run, and the GitHub
      thread for the token — absent everywhere.
- [ ] Kill the process mid-task, restart, re-trigger → works.
- [ ] Set `AIZU_MAX_RUNS_PER_HOUR=1`, trigger twice → second politely
      refused.
- [ ] README demo GIF/asciinema of a real issue→PR round trip (the docs
      site already ships asciinema assets — use them).
- [ ] Read the README top to bottom once as a stranger; every command must
      be copy-pasteable in order.

## Post-launch (fast-follows)

| Order | Plan | Notes |
|-------|------|-------|
| 12 | [feat-engine-presets.md](feat-engine-presets.md) | `AIZU_ENGINE=claude\|aider\|opencode` widens the funnel — the natural "week after launch" ship, and a good response to the inevitable "does it work with X?" comments. Coordinate with plan 11: presets should reference published ghcr images. |
| 13 | [feat-worker-concurrency.md](feat-worker-concurrency.md) | Matters once several people share one instance — post-launch by definition. Depends on plan 2's single-process assumption (documented there). |
| 14 | [chore-code-quality.md](chore-code-quality.md) | Anytime filler between larger plans; each of its four items is independent, and its header says to skip items already done in passing. |

## Cross-cutting dependency notes

- **Poller file contention:** plans 1, 5, and 7 all edit
  `internal/poller/poller.go`. Do them strictly sequentially (they are
  ordered that way above); rebase between.
- **Worker/client contention:** plans 4, 8, 9, and 10 all touch
  `internal/worker/worker.go` and/or `internal/github/client.go` —
  sequential, in the listed order.
- **`envExports` / helpers.go:** touched by plans 3, 8, 9, and 12 — the
  listed order applies each change to the previous plan's final shape
  (plan 9 ultimately replaces `envExports` with container-env delivery).
- **README/quickstart:** plans 5, 6, 7, 11, and 12 each edit the
  quickstart. Conflicts are trivial (docs), but re-read the full quickstart
  end-to-end after plan 11 and again after plan 12 — per-plan edits leave
  seams.
- **miniredis:** first introduced by whichever of plans 1/2 lands first;
  later plans reuse it. Test-only dependency.

## Explicitly deferred (revisit only after real users exist)

- **Webhook mode** — polling behind NAT with zero inbound networking *is
  the product*; don't trade it for webhook setup friction.
- **GitHub App auth** — more setup, not less, for individuals. Revisit when
  an enterprise actually asks.
- **Hosted/multi-tenant anything** — the moat is "your machine, your
  models".
- **`aizu init` setup wizard** — after milestone 1, `.env` is two required
  lines; a wizard is likely overkill. Re-judge after launch feedback.
