package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/samhornstein/aizu/internal/config"
)

const promptFile = "/tmp/.aizu-prompt"

// errTimedOut reports that a command was killed by its timeout.
var errTimedOut = errors.New("command timed out")

// run executes a shell command on the host. If timeout > 0 the command is
// killed when it elapses and the returned error wraps errTimedOut.
func run(cmd string, timeout time.Duration) (string, error) {
	return runWithStdin(cmd, "", timeout)
}

// runWithStdin is run with the command's stdin fed from a string — the
// channel for anything secret, which must never appear in argv (visible in
// the host process table).
func runWithStdin(cmd, stdin string, timeout time.Duration) (string, error) {
	slog.Debug("exec", "cmd", cmd)
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	// Without WaitDelay, CombinedOutput blocks until every process holding
	// the output pipe exits — a grandchild of sh can outlive the kill and
	// stall the timeout for the child's full duration.
	c.WaitDelay = time.Second
	out, err := c.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("%w after %s", errTimedOut, timeout)
	}
	return string(out), err
}

// resolveEngineCommand picks the engine command for this run — the local
// variant when a local model server is configured and the preset has one —
// and substitutes {prompt_file}, {model} and {base_url}. discover maps a base
// URL to the server's model ID and is only called when the command needs it.
// {base_url} is the configured server rewritten for use inside the sandbox,
// for engines that take the endpoint via env or flag (e.g. aider).
func resolveEngineCommand(cfg *config.Config, discover func(string) (string, error)) (string, error) {
	command := cfg.EngineCommand
	if cfg.OpenAIBaseURL != "" && cfg.EngineLocalCommand != "" {
		command = cfg.EngineLocalCommand
	}
	command = strings.Replace(command, "{prompt_file}", promptFile, 1)
	if strings.Contains(command, "{model}") {
		id, err := discover(cfg.OpenAIBaseURL)
		if err != nil {
			return "", fmt.Errorf("discover local model: %w", err)
		}
		command = strings.ReplaceAll(command, "{model}", shellQuote(id))
	}
	if strings.Contains(command, "{base_url}") {
		command = strings.ReplaceAll(command, "{base_url}", shellQuote(sandboxURL(cfg.OpenAIBaseURL)))
	}
	return command, nil
}

// sandboxURL rewrites a localhost base URL to host.docker.internal for use
// inside an agent container, where localhost is the container itself. URLs
// pointing anywhere else pass through unchanged.
func sandboxURL(base string) string {
	base = strings.Replace(base, "://localhost", "://host.docker.internal", 1)
	return strings.Replace(base, "://127.0.0.1", "://host.docker.internal", 1)
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// buildEnvFile renders the sandbox environment as KEY=value lines for
// `docker run --env-file /dev/stdin`: a fixed git identity plus every
// credential the agent needs. Delivered via stdin so secrets never appear in
// host argv. The env-file format has no quoting, so values containing
// newlines cannot be represented and are skipped.
func buildEnvFile(cfg *config.Config) string {
	vars := []struct{ key, val string }{
		{"GIT_AUTHOR_NAME", "aizu"},
		{"GIT_AUTHOR_EMAIL", "aizu@noreply"},
		{"GIT_COMMITTER_NAME", "aizu"},
		{"GIT_COMMITTER_EMAIL", "aizu@noreply"},
		// gh reads GH_TOKEN first, then GITHUB_TOKEN; other tools read
		// GITHUB_TOKEN — export both.
		{"GITHUB_TOKEN", cfg.GitHubToken},
		{"GH_TOKEN", cfg.GitHubToken},
		{"ANTHROPIC_API_KEY", cfg.AnthropicKey},
		{"OPENAI_API_KEY", cfg.OpenAIKey},
	}
	var b strings.Builder
	for _, v := range vars {
		if v.val == "" || strings.ContainsAny(v.val, "\n\r") {
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", v.key, v.val)
	}
	return b.String()
}
