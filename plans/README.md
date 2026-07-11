# Aizu Improvement Plans

All 15 implementation plans in this directory have been executed and merged
(July 2026) — each as its own branch/PR, in the order laid out in
[ROADMAP.md](ROADMAP.md). The roadmap remains for its milestone history,
philosophy guardrails, and the v0.1 / launch checklists.

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
- **Engine presets wave 2** — `aider` and `opencode` presets (the mechanism
  and templates layout are in place; add a Dockerfile, a preset entry, a
  compose service, and a release publish step per engine).
