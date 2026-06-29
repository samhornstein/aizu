package executor

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/samhornstein/aizu/internal/config"
)

const promptFile = "/tmp/.aizu-prompt"

// run executes a shell command on the host, optionally wrapped in `timeout`.
func run(cmd string, timeout time.Duration) (string, error) {
	slog.Debug("exec", "cmd", cmd)
	var c *exec.Cmd
	if timeout > 0 {
		c = exec.Command("sh", "-c", fmt.Sprintf("timeout %d sh -c %s", int(timeout.Seconds()), shellQuote(cmd)))
	} else {
		c = exec.Command("sh", "-c", cmd)
	}
	out, err := c.CombinedOutput()
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
		"MODEL_SERVER_HOST": cfg.ModelServerHost,
	} {
		if val != "" {
			exports = append(exports, fmt.Sprintf("export %s=%s", key, shellQuote(val)))
		}
	}
	return strings.Join(exports, " && ")
}
