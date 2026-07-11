package executor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samhornstein/aizu/internal/config"
)

type containerExecutor struct {
	cfg *config.Config
}

// credentialHelper authenticates git against GitHub using the GITHUB_TOKEN
// env var present inside the container, so no URL or config file carries the
// token itself.
const credentialHelper = `!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f`

func (e *containerExecutor) Create(repo, branch string, prNumber int) (string, error) {
	sid := "aizu-" + uuid.New().String()[:8]

	// --add-host makes host.docker.internal resolve on Linux too (it is
	// built in on Docker Desktop), so the agent can reach model servers
	// running on the host. Secrets travel as container env via stdin
	// (--env-file /dev/stdin) so they never appear in host argv.
	create := fmt.Sprintf("docker run -d --name=%s --label=aizu=true --add-host=host.docker.internal:host-gateway --memory=4g --cpus=2 --env-file /dev/stdin %s sleep infinity",
		shellQuote(sid), shellQuote(e.cfg.ContainerImage))
	if _, err := runWithStdin(create, buildEnvFile(e.cfg), 0); err != nil {
		return "", fmt.Errorf("docker run: %w", err)
	}

	// Authenticate git through a credential helper reading GITHUB_TOKEN from
	// the container env, instead of a token-in-URL — keeps the token out of
	// host argv and out of .git/config.
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo)
	clone := fmt.Sprintf("git -c credential.helper=%s clone %s /workspace/repo",
		shellQuote(credentialHelper), shellQuote(cloneURL))
	if _, err := e.exec(sid, clone, 0); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}
	config := fmt.Sprintf("cd /workspace/repo && git config user.name aizu && git config user.email aizu@noreply && git config credential.helper %s",
		shellQuote(credentialHelper))
	if _, err := e.exec(sid, config, 0); err != nil {
		return "", fmt.Errorf("git config: %w", err)
	}
	if prNumber > 0 {
		// refs/pull/<n>/head exists in the base repo for fork PRs too. Not
		// the pull/N/head:<branch> refspec form: git refuses to fetch into
		// the checked-out branch, which breaks when the PR's head ref equals
		// the default branch. checkout -B resets it regardless.
		fetch := fmt.Sprintf("cd /workspace/repo && git fetch origin %s && git checkout -B %s FETCH_HEAD",
			shellQuote(fmt.Sprintf("pull/%d/head", prNumber)), shellQuote(branch))
		if _, err := e.exec(sid, fetch, 0); err != nil {
			return "", fmt.Errorf("fetch PR #%d: %w", prNumber, err)
		}
	} else if branch != "" {
		if _, err := e.exec(sid, fmt.Sprintf("cd /workspace/repo && git checkout %s", shellQuote(branch)), 0); err != nil {
			return "", fmt.Errorf("git checkout %s: %w", branch, err)
		}
	}

	if err := e.writeModelsJSON(sid); err != nil {
		return "", fmt.Errorf("write models.json: %w", err)
	}

	slog.Info("Created container", "sid", sid, "repo", repo, "branch", branch)
	return sid, nil
}

func (e *containerExecutor) writeModelsJSON(sid string) error {
	// The models.json layout (and its /root/.pi path) is pi-specific; other
	// engines get their model configuration via env/flags. An empty Engine
	// (config built without Load, e.g. in tests) keeps the pi behavior.
	if e.cfg.OpenAIBaseURL == "" || (e.cfg.Engine != "" && e.cfg.Engine != "pi") {
		return nil
	}
	modelID, err := discoverModelID(e.cfg.OpenAIBaseURL)
	if err != nil {
		return fmt.Errorf("discover model: %w", err)
	}
	type modelEntry struct {
		ID string `json:"id"`
	}
	type compat struct {
		SupportsDeveloperRole   bool `json:"supportsDeveloperRole"`
		SupportsReasoningEffort bool `json:"supportsReasoningEffort"`
	}
	type provider struct {
		BaseURL string       `json:"baseUrl"`
		API     string       `json:"api"`
		APIKey  string       `json:"apiKey"`
		Compat  compat       `json:"compat"`
		Models  []modelEntry `json:"models"`
	}
	payload := map[string]interface{}{
		"providers": map[string]provider{
			"local": {
				BaseURL: sandboxURL(e.cfg.OpenAIBaseURL),
				API:     "openai-completions",
				APIKey:  "local",
				Compat:  compat{SupportsDeveloperRole: false, SupportsReasoningEffort: false},
				Models:  []modelEntry{{ID: modelID}},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	_, err = e.exec(sid, fmt.Sprintf("mkdir -p /root/.pi/agent && echo %s | base64 -d > /root/.pi/agent/models.json", shellQuote(encoded)), 0)
	return err
}

func discoverModelID(baseURL string) (string, error) {
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + "/models")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if len(result.Data) == 0 {
		return "", fmt.Errorf("no models returned by %s/models", baseURL)
	}
	return result.Data[0].ID, nil
}

func (e *containerExecutor) RunEngine(sid, prompt string) (int, string, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(prompt))
	if _, err := e.exec(sid, fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(encoded), promptFile), 0); err != nil {
		return 1, "", fmt.Errorf("write prompt: %w", err)
	}

	command, err := resolveEngineCommand(e.cfg, discoverModelID)
	if err != nil {
		return 1, "", err
	}
	// Credentials and git identity are container env (set at docker run);
	// nothing to export here.
	full := fmt.Sprintf("cd /workspace/repo && %s", command)
	slog.Info("Running engine in container", "sid", sid)

	output, err := e.exec(sid, full, time.Duration(e.cfg.Timeout)*time.Second)
	if err != nil {
		// Timeout must be checked before ExitError: a killed process also
		// yields an ExitError, which would otherwise mask the timeout.
		if errors.Is(err, errTimedOut) {
			slog.Warn("Engine timed out", "timeout", e.cfg.Timeout, "sid", sid)
			return 124, fmt.Sprintf("Timed out after %ds", e.cfg.Timeout), nil
		}
		if _, ok := err.(*exec.ExitError); ok {
			return 1, output, nil
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
