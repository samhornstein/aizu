# Roadmap: execution order for the open issues

The 15 original plan documents that used to live here were all executed and
merged (July 2026, culminating in v0.1.0). Plans now live as GitHub issues —
detailed, agent-executable, with anchored file references and explicit
out-of-scope sections. This file orders them.

The goal shaping the order is unchanged: **launch-ready** — safe by default,
installable in five minutes, and it must not embarrass you in front of a
code-reading audience. The current gap is packaging and first-contact
experience, not engineering: everything below either removes a silent
first-use failure, answers a predictable security question, or sharpens the
positioning.

## Philosophy guardrails (apply to every issue)

Carried over from the original roadmap; they still govern:

- **Resist new knobs.** Every env var is documentation surface and a decision
  forced on the user. Prefer a good hardcoded default.
- **Guard the prompt budget.** `AIZU.md` is prepended to every prompt and
  must work with small local models — keep it under ~40 lines.
- **Errors are UX.** Anything a first-run user can get wrong must fail fast
  and say what to do. Silent non-behavior (see #87) is the worst failure mode.
- **Prefer shrinking a plan to adding machinery.** When an implementation
  grows beyond its issue, cut scope rather than generalize.

## Milestone 1 — pre-launch (gates any public sharing)

Work top to bottom. #92 and #83 are strictly last: the release must contain
everything above it, and the demo records against the release.

| Order | Issue | Why it gates launch |
|-------|-------|---------------------|
| 1 | [#87](https://github.com/samhornstein/aizu/issues/87) fix: accept an optional leading `@` before the trigger | The most likely silent first-use failure — `@aizu fix this` is GitHub muscle memory and currently does nothing, with no feedback. One-line core fix; do it first. |
| 2 | [#88](https://github.com/samhornstein/aizu/issues/88) security: harden the agent sandbox (`--pids-limit`, `no-new-privileges`) | Free hardening; a visible answer to the first security questions from a code-reading audience. |
| 3 | [#89](https://github.com/samhornstein/aizu/issues/89) docs: document the docker.sock mount in the threat model | The sharpest omission in the security story — someone will grep the compose file within the hour. Document it before someone else does. After #88 so the docs edit describes the final flag set. |
| 4 | [#90](https://github.com/samhornstein/aizu/issues/90) docs: "Why Aizu" section in the README | Pre-empts the predictable top comment ("why not Copilot / the Claude app?"). Positioning is currently implicit. |
| 5 | [#91](https://github.com/samhornstein/aizu/issues/91) docs: recommend a 7B model in the quickstart | The recommended path must not produce output the README apologizes for. The first reply is the demo. |
| 6 | [#86](https://github.com/samhornstein/aizu/issues/86) feat: pin agent images to the binary's release version | Supply-chain/reproducibility answer; must land before the release so v0.2.0 is the first to ship pinned tags. |
| 7 | [#92](https://github.com/samhornstein/aizu/issues/92) chore: cut the v0.2.0 release | `:latest` images lag main; the no-clone install pulls stale code until this ships. Contains the release checklist. |
| 8 | [#83](https://github.com/samhornstein/aizu/issues/83) add demo GIF to README | For this product category the GIF *is* the pitch. Record a real issue→PR round trip against the v0.2.0 images, not a source build. |

## Milestone 2 — fast follows (post-launch)

Ordered small-to-large and by file contention (notes below). None of these
should hold the launch.

| Order | Issue | Notes |
|-------|-------|-------|
| 9 | [#94](https://github.com/samhornstein/aizu/issues/94) chore: remove the separate poller/worker CLI modes | Removes a misleading knob whose advertised use corrupts dedupe state. Tiny. |
| 10 | [#93](https://github.com/samhornstein/aizu/issues/93) feat: conditional requests (ETags) in the poller | The only real scalability cliff (~10 repos saturates the PAT budget). Changes comment-cursor semantics — the largest single item here. |
| 11 | [#95](https://github.com/samhornstein/aizu/issues/95) feat: surface the dead-letter queue | Startup count log + LTRIM cap + docs. |
| 12 | [#96](https://github.com/samhornstein/aizu/issues/96) chore: output-handling fixes (rune-safe truncate, timeout tail) | Small correctness/debuggability fixes in the worker/executor output path. |
| 13 | [#97](https://github.com/samhornstein/aizu/issues/97) feat: run details in the progress comment | One static metadata line; the cheap version of run visibility. |
| 14 | [#82](https://github.com/samhornstein/aizu/issues/82) feat: per-comment `model=<name>` selection | Widens the local-model story once real users exist to want it. |
| 15 | [#98](https://github.com/samhornstein/aizu/issues/98) refactor: host docker commands as argv, not `sh -c` | Risk reduction, no new capability — natural filler between features. Last so it rebases onto everyone else's executor edits, not vice versa. |

## Deferred (revisit only on real demand)

Open issues kept as markers of prior decisions, not scheduled work:

- [#84](https://github.com/samhornstein/aizu/issues/84) **webhooks** — polling
  behind NAT with zero inbound networking *is the product*; #93 keeps polling
  cheap instead.
- [#85](https://github.com/samhornstein/aizu/issues/85) **GitHub App auth** —
  more setup, not less, for individuals. Revisit when an enterprise asks.
- [#21](https://github.com/samhornstein/aizu/issues/21) **Discussions** —
  dormant; GraphQL-only API vs. the REST-only client.

## Cross-cutting dependency notes

- **`internal/poller/poller.go`:** #87 and #93 both edit it — #87 lands
  first (different milestone), rebase #93 over it.
- **`internal/worker/worker.go`:** #87 (isHelpRequest), #96, and #97 all
  touch it — sequential in the listed order.
- **`internal/executor/`:** #88 (container.go run flags), #96
  (container.go timeout path), #98 (rewrites helpers.go and every
  container.go call site) — #98 deliberately last.
- **README quickstart:** #90 and #91 both edit it (adjacent sections);
  re-read the quickstart end-to-end after both, and again after #92's
  fresh-machine run-through.
- **Release coupling:** #86 → #92 → #83 is a strict chain (pinning is in the
  release; the GIF records the released images).
