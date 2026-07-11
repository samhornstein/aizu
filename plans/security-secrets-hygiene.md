# Security: keep secrets out of process argv and public comments

**Branch:** `fix/secrets-hygiene`
**PR title:** `fix: keep secrets out of command lines and posted output`

## Context

Aizu's executor (`internal/executor/container.go`, `helpers.go`) drives
Docker via host shell commands. Two secret-handling problems:

1. **Secrets in argv.** The GitHub token is embedded in the clone URL
   (`https://x-access-token:<token>@github.com/...`) and passed through
   `docker exec <sid> sh -c '<command>'`, and model API keys are inlined as
   `export OPENAI_API_KEY=...` in the engine command (`envExports`). All of
   these appear in the **host process table** (`ps auxww` shows full argv of
   `sh -c` and `docker exec` commands) for the duration of the command, and
   in any shell tracing. On a single-user machine this is low risk; on a
   shared box, or if Aizu is ever demoed/screenshared, it leaks.

2. **Secrets in posted output.** The worker posts engine output back to
   GitHub (`internal/worker/worker.go`: `formatResult` on success, and the
   error path posts `engine exited N: <output>`). The token is present
   inside the sandbox (remote URL in `.git/config`, and after
   `feat-sandbox-github-api.md`, `GITHUB_TOKEN` in env). An agent that
   echoes its environment or git config — accidentally or because a small
   model does something odd — would publish the token in a public comment.

## Approach

Pass secrets via container environment at create time (through the Docker
API-safe `--env-file /dev/stdin` mechanism, never argv), authenticate git
with a credential helper instead of a token-in-URL, and redact known secrets
from anything posted to GitHub.

**Priority within this plan:** redaction (step 4) is the must-have — it is
the only part that protects against *public* leakage and it is ~20 lines.
The argv/credential-helper work (steps 1–3) protects against local `ps`
snooping on the operator's own machine — real but lower stakes for a
single-user tool. If steps 1–3 balloon in complexity during implementation
(e.g. `--env-file /dev/stdin` misbehaves on some platform), ship step 4
alone and file the rest as a follow-up rather than blocking on it.

### Steps

1. **Add stdin support to `run`.** In `internal/executor/helpers.go`, add a
   variant (or an optional parameter) that feeds stdin:

   ```go
   func runWithStdin(cmd, stdin string, timeout time.Duration) (string, error)
   ```

   Same body as `run` plus `c.Stdin = strings.NewReader(stdin)`. Keep `run`
   delegating to it with empty stdin. (If `bugfix-timeout-detection.md` has
   landed, build on its version of `run`.)

2. **Move secrets to container env at create.** In `container.go`
   `Create()`, change the `docker run` to read an env file from stdin:

   ```go
   create := fmt.Sprintf(
       "docker run -d --name=%s --label=aizu=true --memory=4g --cpus=2 --env-file /dev/stdin %s sleep infinity",
       shellQuote(sid), shellQuote(e.cfg.ContainerImage))
   envFile := buildEnvFile(e.cfg) // KEY=value lines, one per secret + git identity
   if _, err := runWithStdin(create, envFile, 0); err != nil { … }
   ```

   `buildEnvFile` (new, in helpers.go) emits lines for: `GITHUB_TOKEN`,
   `GH_TOKEN`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` (skip empties), plus
   the four `GIT_*` identity vars currently in `envExports`. Env-file format
   is `KEY=value` with no quoting; reject/skip values containing newlines.
   Delete `envExports` and the `full = prefix + " && " + full` wiring in
   `RunEngine` — the env now comes from the container itself. Note:
   `docker exec` does **not** inherit `docker run --env-file` vars into…
   actually it does — `docker exec` runs inside the container's env as
   created. Verify with the test in Verification step; this is the crux of
   the change.

3. **Clone without token-in-URL.** In `Create()`, replace the credentialed
   clone URL with a plain URL plus an inline credential helper reading the
   env var (which now exists inside the container):

   ```go
   clone := `git -c credential.helper='!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f' clone https://github.com/<repo>.git /workspace/repo`
   ```

   Build it with `fmt.Sprintf`/`shellQuote` for the repo. Also set the same
   helper in the repo config after clone so later `git push`/`git fetch`
   inside the sandbox work without a token in `.git/config`:

   ```
   cd /workspace/repo && git config credential.helper '!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f'
   ```

   Result: the token never appears in argv on the host (it's in the
   container's env, delivered via stdin) and no longer sits in
   `.git/config` as plaintext URL.

4. **Redact outgoing comments.** In `internal/worker/worker.go`:

   ```go
   // redact masks configured secrets in text before it is posted publicly.
   func redact(text string, secrets ...string) string {
       for _, s := range secrets {
           if s != "" {
               text = strings.ReplaceAll(text, s, "[redacted]")
           }
       }
       return text
   }
   ```

   Apply it in `reply()` — the single choke point through which both success
   and failure comments flow. The Worker doesn't currently hold the config;
   add a `secrets []string` field set in `New(...)` from
   `cfg.GitHubToken, cfg.AnthropicKey, cfg.OpenAIKey` (plumb `cfg` through
   `main.go`'s `worker.New` call).

5. Keep `writeModelsJSON` and the prompt write as-is (they already pass
   payloads base64-encoded; the models.json contains only `apiKey: "local"`).

## Files to modify

- `internal/executor/helpers.go`
- `internal/executor/container.go`
- `internal/worker/worker.go`
- `main.go` (worker.New signature)
- `internal/worker/worker_test.go`, `internal/executor/helpers_test.go`
- `e2e/e2e_test.go` only if worker.New's signature change touches it

## Tests

- `helpers_test.go`: `buildEnvFile` output — includes set keys, skips empty
  ones, skips values with newlines; `runWithStdin("cat", "hello", …)` returns
  `hello`.
- `worker_test.go`: a task whose engine output contains the configured token
  results in a posted comment containing `[redacted]` and not the token —
  assert via the fake GitHub server body. Cover the error path too (engine
  exit 1 with token in output).

## Verification

```sh
make build && go test -race ./...
docker compose build agent && docker compose up -d --build
```

Then trigger a task and, while the agent container is running:

```sh
ps auxww | grep -c 'x-access-token'   # must be 0
docker exec <aizu-...> sh -c 'echo $GITHUB_TOKEN | head -c 8'  # env present in exec sessions
docker exec <aizu-...> git -C /workspace/repo config remote.origin.url  # no token in URL
```

Confirm `git push` from inside the sandbox still works (trigger an issue
task that opens a PR).

## Out of scope

- GitHub App / short-lived tokens (future idea).
- Removing the token from the sandbox entirely — the agent needs it to push
  and (post `feat-sandbox-github-api.md`) call the API.
- `docker inspect` still shows container env to anyone with Docker-socket
  access; that's equivalent to root and out of scope.
