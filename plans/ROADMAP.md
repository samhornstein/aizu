# Roadmap: milestones and implementation order

The product goal shapes this order: **a low-friction way for an individual
developer to talk to a locally running LLM through GitHub** — private
projects first, enterprise repos later — with a quickstart minimal enough
to attract users.

Two milestones define the cut lines:

- **v0.1 — share with friends & coworkers.** Trusted users, mostly private
  or personal repos, willing to clone and build. The bar: *it must not
  misbehave* (no loops, no silently bricked issues, no cryptic failures)
  and the worst setup friction must be gone.
- **HN launch.** Strangers, public repos, and an audience that will read
  the code and probe the security model within the hour. The bar: *safe by
  default, installable in five minutes, and it must not embarrass you.*

Work top to bottom. Within a milestone, the listed order avoids merge
conflicts; don't reorder across milestone boundaries without checking the
dependency notes at the end.

## Philosophy guardrails (apply to every plan)

Aizu's value is being a **simple, intuitive tool** — a small codebase a
curious user can read in an afternoon, and a setup that mostly configures
itself. When implementing any plan:

- **Resist new knobs.** Every env var is documentation surface and a
  decision forced on the user. A plan that adds one must earn it; prefer a
  good hardcoded default. (The plans below add four:
  `AIZU_ALLOW_ALL`, `AIZU_MAX_RUNS_PER_HOUR`, `AIZU_CONCURRENCY`,
  `AIZU_ENGINE` — each was weighed. Do not add more in passing.)
- **Guard the prompt budget.** Several plans append to `AIZU.md`, which is
  prepended to every prompt and must work with *small local models*. Keep
  it under ~40 lines; after Milestone 2, re-read it top to bottom and cut.
- **Errors are UX.** Anything a first-run user can get wrong must fail
  fast and say what to do — that's why the self-check plan sits in v0.1.
- **Prefer shrinking a plan to adding machinery.** Plans state their own
  Out-of-scope; when an implementation grows beyond its plan, stop and
  cut scope rather than generalize (see the priority notes inside
  `security-secrets-hygiene.md` and `feat-engine-presets.md`).

## Milestone 1 — v0.1 (friends & coworkers)

| Order | Plan | Why it gates v0.1 |
|-------|------|-------------------|
| 1 | [bugfix-issue-retrigger-loop.md](bugfix-issue-retrigger-loop.md) | Critical: issue-body triggers loop forever, burning tokens and spamming threads — the fastest way to lose your first users. Also introduces the Redis seen-markers that the single-account plan leans on. |
| 2 | [bugfix-stale-running-set.md](bugfix-stale-running-set.md) | A crash silently bricks an issue forever; "it just stopped responding" is unrecoverable trust damage with early users. Creates `internal/queue/queue_test.go` used by later plans. |
| 3 | [bugfix-timeout-detection.md](bugfix-timeout-detection.md) | The timeout path is dead code today — long runs surface as inscrutable generic failures. Small fix; also rewrites `run()`, which the secrets plan builds on. |
| 4 | [feat-single-account-mode.md](feat-single-account-mode.md) | "Create a second GitHub account" is the step where a curious friend gives up. Requires plan 1's seen-markers. |
| 5 | [feat-ollama-autodetect.md](feat-ollama-autodetect.md) | Friends run Ollama, not llama.cpp; auto-detection deletes the `OPENAI_BASE_URL` step. |
| 6 | [feat-startup-selfcheck.md](feat-startup-selfcheck.md) | A bad token today logs a warning and keeps running; a typo'd repo logs a 404 every 15s forever. For a "quick to set up" tool, misconfiguration feedback *is* the product. Small plan; it's what makes the other five feel finished. |

**v0.1 exit criteria** — then run the Release workflow and tag `v0.1.0`:

- [ ] All six plans merged; quickstart re-tested from scratch on a clean
      checkout (clone + build is still the install path — fine for this
      audience).
- [ ] Sabotage test: wrong token, then wrong repo name — both produce one
      clear, actionable message, not log spam.
- [ ] One coworker who is not you completes the quickstart with no help.
- [ ] README carries a temporary warning: **do not watch public repos yet**
      (anyone who can comment can trigger the agent until the collaborator
      gate lands in Milestone 2).

*Optional if a spare hour appears:* the `aizu help` section (§2) of
[feat-feedback-polish.md](feat-feedback-polish.md) — independent, tiny, and
disproportionately useful for people just discovering the tool.

## Milestone 2 — HN launch

Do **all of these before any public sharing** (HN, company-wide, watching
public repos). Milestone 1 makes it good; this milestone makes it safe by
default and installable in minutes.

| Order | Plan | Why it gates the launch |
|-------|------|-------------------------|
| 7 | [bugfix-fork-pr-checkout.md](bugfix-fork-pr-checkout.md) | Fork PRs are a stranger/public-repo phenomenon — exactly the launch audience (coworkers on individual projects rarely fork). First in this milestone because it changes the `Executor` interface and `buildPrompt`, which plans 8–10 then build on. |
| 8 | [security-collaborator-gate.md](security-collaborator-gate.md) | Today anyone who can comment can run an agent on your machine. The first question every HN commenter will ask; must be the default, not a setting. Removes the v0.1 README warning. |
| 9 | [feat-sandbox-github-api.md](feat-sandbox-github-api.md) | Makes AIZU.md's promised progress updates real (the demo depends on this looking alive). Ordered before the secrets plan because it adds the `GITHUB_TOKEN` export that plan then secures. |
| 10 | [security-secrets-hygiene.md](security-secrets-hygiene.md) | Redacts secrets from anything posted to GitHub (the must-have — see the priority note inside the plan) and keeps tokens out of host `ps`. Builds on plans 3 and 9. |
| 11 | [feat-feedback-polish.md](feat-feedback-polish.md) | §4 (rate limit) is **required** — the backstop against runaway runs when strangers can see your bot. §1 (reactions), §2 (help), §3 (progress comment) are what make the demo GIF feel polished; do all four unless time-boxed, in which case §4 > §3 > §2 > §1. |
| 12 | [feat-prebuilt-images.md](feat-prebuilt-images.md) | The HN install must be: curl one compose file, two-line `.env`, `docker compose up`. Last before launch so the published image and rewritten README contain everything above. Cut a release after merging. |

**Launch checklist** — after plan 12's release:

- [ ] Fresh-machine quickstart run-through, timed — target under 5 minutes
      from `curl` to first agent reply.
- [ ] Trigger from a non-collaborator account on a public repo → denied,
      logged, no reply.
- [ ] `grep` a full task's logs, `ps` output during a run, and the GitHub
      thread for the token — absent everywhere.
- [ ] Kill the process mid-task, restart, re-trigger → works.
- [ ] Set `AIZU_MAX_RUNS_PER_HOUR=1`, trigger twice → second politely
      refused.
- [ ] Re-read `AIZU.md` end to end; cut it back under ~40 lines if the
      milestone's edits bloated it (small local models must still follow it).
- [ ] README demo GIF/asciinema of a real issue→PR round trip (the docs
      site already ships asciinema assets — use them).
- [ ] Read the README top to bottom once as a stranger; every command must
      be copy-pasteable in order.

## Post-launch (fast-follows)

| Order | Plan | Notes |
|-------|------|-------|
| 13 | [feat-engine-presets.md](feat-engine-presets.md) | `AIZU_ENGINE=claude` widens the funnel — the natural "week after launch" ship and a good answer to "does it work with X?" comments. Wave 1 is pi + claude only (see the scope note in the plan); coordinate with plan 12 so presets reference published ghcr images. |
| 14 | [feat-worker-concurrency.md](feat-worker-concurrency.md) | Matters once several people share one instance — post-launch by definition. Depends on plan 2's single-process assumption (documented there). |
| 15 | [chore-code-quality.md](chore-code-quality.md) | Anytime filler between larger plans; each of its four items is independent, and its header says to skip items already done in passing. |

## Cross-cutting dependency notes

- **Poller file contention:** the retrigger fix, single-account mode, the
  self-check, and the collaborator gate all edit
  `internal/poller/poller.go`. Do them strictly sequentially (they are
  ordered that way above); rebase between.
- **Worker/client contention:** fork-PR checkout, sandbox GitHub API,
  secrets hygiene, and feedback polish all touch
  `internal/worker/worker.go` and/or `internal/github/client.go` —
  sequential, in the listed order (7 → 9 → 10 → 11).
- **`envExports` / helpers.go:** touched by the timeout fix, sandbox
  GitHub API, secrets hygiene, and engine presets — the listed order
  applies each change to the previous plan's final shape (secrets hygiene
  ultimately replaces `envExports` with container-env delivery).
- **README/quickstart:** single-account, ollama-autodetect, collaborator
  gate, prebuilt images, and engine presets each edit the quickstart.
  Conflicts are trivial (docs), but re-read the full quickstart end-to-end
  after plan 12 and again after plan 13 — per-plan edits leave seams.
- **miniredis:** first introduced by whichever of plans 1/2 lands first;
  later plans reuse it. Test-only dependency.

## Explicitly deferred (revisit only after real users exist)

- **Webhook mode** — polling behind NAT with zero inbound networking *is
  the product*; don't trade it for webhook setup friction.
- **GitHub App auth** — more setup, not less, for individuals. Revisit when
  an enterprise actually asks.
- **Hosted/multi-tenant anything** — the moat is "your machine, your
  models".
- **`aizu init` setup wizard** — after Milestone 1, `.env` is two required
  lines and misconfiguration fails fast with instructions; a wizard is
  likely overkill. Re-judge after launch feedback.
- **Auto-discovering `AIZU_REPOS`** (watch all the token's repos when
  unset) — tempting quickstart deletion, but surprising behavior and heavy
  API use. Explicit repo lists are the intuitive choice here.
