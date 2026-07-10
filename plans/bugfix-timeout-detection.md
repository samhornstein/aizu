# Fix: engine timeout detection is fragile

**Branch:** `fix/timeout-detection`
**PR title:** `fix: use Go-native timeouts for engine runs`

## Context

Aizu's executor (`internal/executor/`) runs the agent engine via
`docker exec` on the host. Timeouts are implemented in
`internal/executor/helpers.go` `run()` by wrapping the command in the shell
`timeout` utility:

```go
c = exec.Command("sh", "-c", fmt.Sprintf("timeout %d sh -c %s", int(timeout.Seconds()), shellQuote(cmd)))
```

and detected in `internal/executor/container.go` `RunEngine()` by string
matching:

```go
if strings.Contains(err.Error(), "signal: killed") {
```

**Problems:**

1. The production image is Alpine (`Dockerfile`, runtime stage
   `alpine:3.21`), whose busybox `timeout` sends **SIGTERM** by default — the
   Go error is then `"signal: terminated"` (or `exit status 143` if the shell
   catches it), so the `"signal: killed"` check misses, and a timeout is
   misreported to the user as a generic engine failure with exit 1 instead of
   the intended "Timed out after Ns" message with exit 124.
2. Behavior differs between dev (macOS/coreutils) and prod (busybox), and
   relies on `timeout` existing at all.
3. String-matching error text is brittle.

Note: killing the host-side `docker exec` does not kill the process inside
the container; that's acceptable here because the worker calls
`Destroy(sid)` (docker rm -f) right after, which kills everything.

## Approach

Replace the shell `timeout` wrapper with Go-native
`exec.CommandContext` + `context.WithTimeout`, and detect timeouts by
checking the context.

### Steps

1. Rewrite `run` in `internal/executor/helpers.go`:

   ```go
   // errTimedOut reports that the command was killed by its timeout.
   var errTimedOut = errors.New("command timed out")

   // run executes a shell command on the host. If timeout > 0 the command is
   // killed when it elapses and errTimedOut is returned (wrapped).
   func run(cmd string, timeout time.Duration) (string, error) {
       slog.Debug("exec", "cmd", cmd)
       ctx := context.Background()
       if timeout > 0 {
           var cancel context.CancelFunc
           ctx, cancel = context.WithTimeout(ctx, timeout)
           defer cancel()
       }
       c := exec.CommandContext(ctx, "sh", "-c", cmd)
       out, err := c.CombinedOutput()
       if ctx.Err() == context.DeadlineExceeded {
           return string(out), fmt.Errorf("%w after %s", errTimedOut, timeout)
       }
       return string(out), err
   }
   ```

   Imports: add `context`, `errors`; keep the rest. `exec.CommandContext`
   sends SIGKILL to the `sh` process on deadline. (Go ≥1.20 also offers
   `c.Cancel`/`WaitDelay`; not needed — default kill is fine since Destroy
   cleans up the container side.)

2. In `internal/executor/container.go` `RunEngine()`, replace the
   `strings.Contains(err.Error(), "signal: killed")` branch with:

   ```go
   if errors.Is(err, errTimedOut) {
       slog.Warn("Engine timed out", "timeout", e.cfg.Timeout, "sid", sid)
       return 124, fmt.Sprintf("Timed out after %ds", e.cfg.Timeout), nil
   }
   ```

   Add `errors` to imports; remove `strings` only if now unused (it is still
   used for `strings.Replace` — keep it).

3. Check the ordering of the existing branches: the `*exec.ExitError` check
   currently comes first. A killed process also yields an `*exec.ExitError`,
   so the timeout check must come **before** the ExitError check. Reorder:

   ```go
   output, err := e.exec(sid, full, time.Duration(e.cfg.Timeout)*time.Second)
   if err != nil {
       if errors.Is(err, errTimedOut) { ... return 124 ... }
       if _, ok := err.(*exec.ExitError); ok {
           return 1, output, nil
       }
       return 1, output, err
   }
   ```

## Files to modify

- `internal/executor/helpers.go`
- `internal/executor/container.go`
- `internal/executor/helpers_test.go`

## Tests

Extend `internal/executor/helpers_test.go` (plain `go test`, no Docker
needed — `run` executes host shell commands):

1. `run("sleep 5", 100*time.Millisecond)` returns an error satisfying
   `errors.Is(err, errTimedOut)` in well under 5s.
2. `run("echo hi", time.Second)` succeeds with output `hi\n`.
3. `run("exit 3", time.Second)` returns an `*exec.ExitError`, not
   errTimedOut.
4. `run("echo hi", 0)` (no timeout) still works.

## Verification

```sh
make build
go test -race ./...
```

Manual (optional): set `AIZU_TIMEOUT=5` in `.env`, use an
`ENGINE_COMMAND=sleep 60` override, trigger a task, and confirm the GitHub
reply says the run timed out (exit 124 path) rather than a generic engine
failure.

## Out of scope

- Do not thread real `context.Context` through the Executor interface (nice
  refactor, separate change).
- Do not change how the container-side process is cleaned up (Destroy already
  handles it).
- Do not touch the engine command construction or models.json logic.
