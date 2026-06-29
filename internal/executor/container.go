package executor

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samhornstein/aizu/internal/config"
)

type containerExecutor struct {
	cfg *config.Config
}

func (e *containerExecutor) Create(repo, branch string) (string, error) {
	sid := "aizu-" + uuid.New().String()[:8]

	create := fmt.Sprintf("docker run -d --name=%s --label=aizu=true --memory=4g --cpus=2 %s sleep infinity",
		shellQuote(sid), shellQuote(e.cfg.ContainerImage))
	if _, err := run(create, 0); err != nil {
		return "", fmt.Errorf("docker run: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo)
	if e.cfg.GitHubToken != "" {
		cloneURL = fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", e.cfg.GitHubToken, repo)
	}
	if _, err := e.exec(sid, fmt.Sprintf("git clone %s /workspace/repo", shellQuote(cloneURL)), 0); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}
	if _, err := e.exec(sid, "cd /workspace/repo && git config user.name aizu && git config user.email aizu@noreply", 0); err != nil {
		return "", fmt.Errorf("git config: %w", err)
	}
	if branch != "" {
		if _, err := e.exec(sid, fmt.Sprintf("cd /workspace/repo && git checkout %s", shellQuote(branch)), 0); err != nil {
			return "", fmt.Errorf("git checkout %s: %w", branch, err)
		}
	}

	slog.Info("Created container", "sid", sid, "repo", repo, "branch", branch)
	return sid, nil
}

func (e *containerExecutor) RunEngine(sid, prompt string) (int, string, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(prompt))
	if _, err := e.exec(sid, fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(encoded), promptFile), 0); err != nil {
		return 1, "", fmt.Errorf("write prompt: %w", err)
	}

	command := strings.Replace(e.cfg.EngineCommand, "{prompt_file}", promptFile, 1)
	full := fmt.Sprintf("cd /workspace/repo && %s", command)
	if prefix := envExports(e.cfg); prefix != "" {
		full = prefix + " && " + full
	}
	slog.Info("Running engine in container", "sid", sid)

	output, err := e.exec(sid, full, time.Duration(e.cfg.Timeout)*time.Second)
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return 1, output, nil
		}
		if strings.Contains(err.Error(), "signal: killed") {
			slog.Warn("Engine timed out", "timeout", e.cfg.Timeout, "sid", sid)
			return 124, fmt.Sprintf("Timed out after %ds", e.cfg.Timeout), nil
		}
		return 1, output, err
	}
	return 0, output, nil
}

func (e *containerExecutor) ReadFile(sid, path string) (string, error) {
	return e.exec(sid, fmt.Sprintf("cat %s 2>/dev/null", shellQuote(path)), 0)
}

func (e *containerExecutor) Destroy(sid string) {
	_, _ = run(fmt.Sprintf("docker rm -f %s", shellQuote(sid)), 0)
	slog.Info("Destroyed container", "sid", sid)
}

func (e *containerExecutor) CleanupStale() {
	output, err := run("docker ps -a --filter label=aizu=true --format '{{.Names}}'", 0)
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.HasPrefix(name, "aizu-") {
			slog.Info("Cleaning up stale container", "name", name)
			e.Destroy(name)
		}
	}
}

func (e *containerExecutor) exec(sid, command string, timeout time.Duration) (string, error) {
	return run(fmt.Sprintf("docker exec %s sh -c %s", shellQuote(sid), shellQuote(command)), timeout)
}
