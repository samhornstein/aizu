# Feature: engine presets — one variable to pick your agent

**Branch:** `feat/engine-presets`
**PR title:** `feat: add AIZU_ENGINE presets for common coding agents`

## Context

Aizu runs a coding agent inside a sandbox image. The engine is configurable
but only via two low-level knobs: `CONTAINER_IMAGE` (which image) and
`ENGINE_COMMAND` (what to run in it, with `{prompt_file}` substituted) —
defaults in `internal/config/config.go` point at the pi engine
(`aizu-agent:pi`, `pi -p "$(cat {prompt_file})"`), built from
`templates/pi/Dockerfile`. The `templates/` layout and the compose comment
("To use a different agent, add templates/<engine>/Dockerfile…") already
anticipate multiple engines, but a user wanting Claude Code or Aider must
hand-write a Dockerfile and both env vars.

**Goal:** `AIZU_ENGINE=claude` (or `aider`, `opencode`; default `pi`) does
it all — much friendlier, and Claude Code / Aider presets widen the funnel
to users who already have those tools' credentials.

⚠️ **Implementation-time caution:** third-party CLI install methods and
flags change. The commands below were correct as of mid-2026 — verify each
against the tool's current docs before committing, and prefer pinning
install versions.

## Approach

### Steps

1. **Preset table.** In `internal/config/config.go`:

   ```go
   // enginePreset bundles the sandbox image and run command for a known agent.
   type enginePreset struct {
       Image   string
       Command string
   }

   var enginePresets = map[string]enginePreset{
       "pi":       {"aizu-agent:pi", `pi -p "$(cat {prompt_file})"`},
       "claude":   {"aizu-agent:claude", `claude --dangerously-skip-permissions -p "$(cat {prompt_file})"`},
       "aider":    {"aizu-agent:aider", `aider --yes-always --message-file {prompt_file}`},
       "opencode": {"aizu-agent:opencode", `opencode run "$(cat {prompt_file})"`},
   }
   ```

   In `Load()`: add `Engine string` to Config (default `"pi"`), read
   `AIZU_ENGINE`, and resolve the preset **before** the `CONTAINER_IMAGE` /
   `ENGINE_COMMAND` env overrides so those still win as expert-level
   overrides of a preset. Unknown engine → log the valid names and fall back
   to `pi` (don't exit; config.Load has no error path today — keep it that
   way).

   If `feat-prebuilt-images.md` has landed, preset image names should be the
   ghcr references (`ghcr.io/samhornstein/aizu-agent-<engine>:latest`) and
   that plan's publish matrix must grow to cover each template — coordinate.

2. **Templates.** Add one Dockerfile per engine, modeled on
   `templates/pi/Dockerfile` (node:24-bookworm-slim base + git/ripgrep/ca-
   certificates; keep `--ignore-scripts` where possible):

   - `templates/claude/Dockerfile` — `npm install -g @anthropic-ai/claude-code`.
     Needs `ANTHROPIC_API_KEY` (already exported to the sandbox by
     `envExports` in `internal/executor/helpers.go`).
   - `templates/aider/Dockerfile` — Python base (`python:3.12-slim-bookworm`)
     plus git; `pip install --no-cache-dir aider-chat`. Aider reads
     `OPENAI_API_KEY`/`ANTHROPIC_API_KEY`/`OPENAI_API_BASE` — note
     `OPENAI_API_BASE` vs Aizu's `OPENAI_BASE_URL`; export both names in
     `envExports` when `cfg.OpenAIBaseURL` is set (small addition there).
   - `templates/opencode/Dockerfile` — `npm install -g opencode-ai`.

3. **Compose build entries.** In `docker-compose.yml`, add one service per
   engine mirroring the existing `agent` service (build context
   `./templates/<engine>`, image `aizu-agent:<engine>`, `profiles:
   ["agent"]`). Rename existing `agent` → `agent-pi` and keep `agent` as an
   alias is not possible in compose; instead name them `agent-pi`,
   `agent-claude`, … and update the Makefile:

   ```make
   ENGINE ?= pi
   build-agent:
   	docker compose build agent-$(ENGINE)
   ```

   Update the compose header comment and the `docker compose build agent`
   references in README/docs to `make build-agent` (or
   `docker compose build agent-pi`).

4. **Model-ID injection generalization.** `RunEngine`
   (`internal/executor/container.go`) currently rewrites the command with
   `strings.Replace(command, "pi ", "pi --model "+…)` — a pi-only hack that
   would corrupt other engines' commands (and silently no-op for them).
   Replace it with a `{model}` placeholder mechanism: presets that support
   local model selection include `{model}` in their Command (pi's becomes
   `pi --model {model} -p "$(cat {prompt_file})"` — but only when a local
   base URL is in play, so make the substitution: if `{model}` is present
   and a model was discovered, substitute it; if `{model}` is present and
   none was discovered, substitute the empty string *including removing the
   flag* — simplest: use two command variants per preset, `Command` and
   `LocalCommand`, choosing `LocalCommand` when `cfg.OpenAIBaseURL != ""`.
   Pick whichever is less code; document the choice in the preset table
   comment. `writeModelsJSON` is also pi-specific (`/root/.pi/agent/...`) —
   gate it on the engine being `pi` (add `Engine` to what the executor can
   see; it already holds the whole `cfg`).

5. **Docs.** README: a short "Choosing your agent" section with the four
   values and which credential each needs. `.env.example`:

   ```
   # AIZU_ENGINE=pi                             # pi | claude | aider | opencode
   ```

   Best-practices page: note that `CONTAINER_IMAGE`/`ENGINE_COMMAND`
   override presets for custom agents.

## Files to modify

- `internal/config/config.go` (+ `config_test.go`)
- `internal/executor/container.go`, `internal/executor/helpers.go`
  (+ tests)
- `templates/claude/Dockerfile`, `templates/aider/Dockerfile`,
  `templates/opencode/Dockerfile` (new)
- `docker-compose.yml`, `Makefile`
- `.env.example`, `README.md`, `docs/content/docs/**`

## Tests

- `config_test.go`: default engine pi; `AIZU_ENGINE=claude` sets both image
  and command; unknown engine falls back to pi; explicit `ENGINE_COMMAND`
  env beats the preset.
- Executor tests: model substitution logic for a command with/without the
  placeholder; `writeModelsJSON` skipped for non-pi engines.

## Verification

```sh
make build && go test -race ./...
for e in pi claude aider opencode; do docker compose build agent-$e; done
```

Manual: `AIZU_ENGINE=claude` + `ANTHROPIC_API_KEY` in `.env`, restart,
trigger a task, and confirm the reply comes from Claude Code (logs show the
`aizu-agent:claude` container). Repeat with pi + a local model to confirm
no regression in the model-injection path.

## Out of scope

- No per-repo engine selection.
- No support matrix testing of every engine × every model provider — pi and
  one other engine verified manually is enough for the PR.
- Local-model (`OPENAI_BASE_URL`) support for engines other than pi may be
  best-effort; document which presets need a real API key.
