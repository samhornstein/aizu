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

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// envExports builds the `export ...` prefix passed to the engine: a fixed git
// identity plus any model credentials that are set.
func envExports(cfg *config.Config) string {
	exports := []string{
		"export GIT_AUTHOR_NAME=aizu",
		"export GIT_AUTHOR_EMAIL=aizu@noreply",
		"export GIT_COMMITTER_NAME=aizu",
		"export GIT_COMMITTER_EMAIL=aizu@noreply",
	}
	for key, val := range map[string]string{
		"ANTHROPIC_API_KEY": cfg.AnthropicKey,
		"OPENAI_API_KEY":    cfg.OpenAIKey,
	} {
		if val != "" {
			exports = append(exports, fmt.Sprintf("export %s=%s", key, shellQuote(val)))
		}
	}
	return strings.Join(exports, " && ")
}
