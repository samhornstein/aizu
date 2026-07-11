# Feature: Ollama support and local model-server auto-detection

**Branch:** `feat/local-model-autodetect`
**PR title:** `feat: auto-detect local model servers and document Ollama first`

## Context

Aizu's target user runs a local LLM. Today the quickstart instructs them to
install and start **llama.cpp** and hand-set
`OPENAI_BASE_URL=http://host.docker.internal:8080/v1`. But the dominant
local-LLM runtime for this audience is **Ollama** (also LM Studio) — both
expose the same OpenAI-compatible API that Aizu already speaks
(`internal/executor/container.go` `discoverModelID` hits
`<base>/models`; `writeModelsJSON` configures the pi engine against it).
Nothing in the code needs a new protocol; this is a discovery + docs gap.

Two improvements:

1. **Auto-detect**: when no model credential is configured at all
   (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENAI_BASE_URL` all empty),
   probe the well-known local ports and use the first server that responds.
   The quickstart then doesn't need the `OPENAI_BASE_URL` step at all for
   most users.
2. **Docs**: document Ollama as the first-choice local server, llama.cpp and
   LM Studio as alternatives.

Networking note: Aizu runs inside a container (`docker-compose.yml`), so
"localhost" model servers on the host are reached via
`host.docker.internal`. That name resolves on Docker Desktop (macOS /
Windows) automatically; on Linux it needs
`extra_hosts: ["host.docker.internal:host-gateway"]` on the aizu service —
add it (harmless on Desktop).

## Approach

### Steps

1. **Add the probe.** New file `internal/config/autodetect.go`:

   ```go
   // wellKnownModelServers are probed in order when no model credential is
   // configured. host.docker.internal reaches the host from inside the
   // aizu container (see extra_hosts in docker-compose.yml).
   var wellKnownModelServers = []string{
       "http://host.docker.internal:11434/v1", // Ollama
       "http://host.docker.internal:8080/v1",  // llama.cpp (llama-server)
       "http://host.docker.internal:1234/v1",  // LM Studio
       "http://localhost:11434/v1",            // same, when running outside Docker
       "http://localhost:8080/v1",
       "http://localhost:1234/v1",
   }

   // AutodetectModelServer probes well-known local OpenAI-compatible servers
   // and returns the first base URL whose /models endpoint answers, or "".
   func AutodetectModelServer() string {
       client := &http.Client{Timeout: 2 * time.Second}
       for _, base := range wellKnownModelServers {
           resp, err := client.Get(base + "/models")
           if err != nil {
               continue
           }
           _ = resp.Body.Close()
           if resp.StatusCode == http.StatusOK {
               return base
           }
       }
       return ""
   }
   ```

   Keep it out of `Load()` — `Load()`'s contract says "performs no network
   calls" (doc comment, config.go). Call it from `main.go` instead, after
   `config.Load()`, only in worker-capable modes:

   ```go
   if cfg.AnthropicKey == "" && cfg.OpenAIKey == "" && cfg.OpenAIBaseURL == "" {
       if base := config.AutodetectModelServer(); base != "" {
           cfg.OpenAIBaseURL = base
           slog.Info("Auto-detected local model server", "base_url", base)
       } else {
           slog.Warn("No model credential configured and no local model server found; agent runs will fail. Set OPENAI_BASE_URL, ANTHROPIC_API_KEY, or OPENAI_API_KEY.")
       }
   }
   ```

   The Warn branch fixes a real support problem: today a missing credential
   fails only when the first task runs, deep in engine output.

2. **URL rewriting for the sandbox.** The detected URL is used in two
   places with different network contexts: `discoverModelID` runs in the
   *aizu container* (host.docker.internal works there once extra_hosts is
   set), and the URL is written into the *agent container's* models.json
   (`writeModelsJSON`), where it must also resolve. Agent containers are
   plain `docker run` (default bridge network) — `host.docker.internal`
   resolves on Docker Desktop but **not** on Linux. Fix generally: add
   `--add-host=host.docker.internal:host-gateway` to the `docker run` line
   in `internal/executor/container.go` `Create()`. If auto-detection chose a
   `localhost` URL (Aizu running natively via `make run`), rewrite it to
   `host.docker.internal` before writing models.json, since localhost inside
   the agent container is the container itself. Put the rewrite in
   `writeModelsJSON`/`RunEngine` via a small helper
   `sandboxURL(base string) string` in the executor package.

3. **Compose change.** In `docker-compose.yml`, aizu service:

   ```yaml
   extra_hosts:
     - "host.docker.internal:host-gateway"
   ```

4. **Docs.** README quickstart "Start a local model" step becomes
   Ollama-first:

   ```sh
   ollama pull qwen2.5-coder:1.5b && ollama serve   # if not already running
   ```

   with llama.cpp and LM Studio as alternatives, and a note that Aizu
   auto-detects whichever is running — `OPENAI_BASE_URL` is only needed for
   non-standard ports/hosts. Mirror in
   `docs/content/docs/getting-started/_index.md` and trim the
   `.env.example` comment.

## Files to modify

- `internal/config/autodetect.go` (new)
- `main.go`
- `internal/executor/container.go`
- `docker-compose.yml`
- `README.md`, `docs/content/docs/getting-started/_index.md`, `.env.example`
- `internal/config/autodetect_test.go` (new), executor tests

## Tests

- `autodetect_test.go`: point `wellKnownModelServers` (make it a `var` so
  tests can swap it) at an `httptest.Server` → returns its URL; at a dead
  port → returns "". No real network in tests.
- Executor: `sandboxURL` rewrites `http://localhost:11434/v1` →
  `http://host.docker.internal:11434/v1` and leaves other hosts untouched.
- Existing `discoverModelID` tests unaffected.

## Verification

```sh
make build && go test -race ./...
```

Manual matrix (the point of the feature — actually do at least the first):

1. macOS + Ollama running, `.env` with only GITHUB_TOKEN/AIZU_REPOS →
   `docker compose up -d`, logs show "Auto-detected local model server …
   11434", and a triggered task completes.
2. Nothing running → startup logs the actionable warning.
3. `OPENAI_BASE_URL` set explicitly → no probing (log line absent).

## Out of scope

- No native Ollama API support (`/api/generate`) — OpenAI-compat only.
- No model *selection* logic beyond the existing first-model pick in
  `discoverModelID`.
- No re-probing at runtime; detection happens once at startup.
