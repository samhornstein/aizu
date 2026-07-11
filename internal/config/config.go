// Package config loads Aizu's configuration from, in increasing order of
// precedence: built-in defaults and environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// Queue
	RedisURL string

	// Trigger
	Trigger  string   // keyword that fires the agent; must begin the comment, e.g. "aizu"
	Repos    []string // "owner/repo" list; empty means auto-discover the token owner's repos
	Users    []string // explicit allowlist of trigger authors; empty defers to repo permissions
	AllowAll bool     // DANGER: let anyone who can comment trigger the agent

	// GitHub
	GitHubToken string // personal access token (PAT)
	BotUsername string // authenticated account login; comments from it are ignored (set at startup)

	// Agent
	ContainerImage string // image the agent runs in
	EngineCommand  string // command run inside the container; {prompt_file} is substituted
	Timeout        int    // agent run timeout, seconds
	MaxRunsPerHour int    // per-repo hourly cap on agent runs; 0 disables

	// Poller
	PollInterval time.Duration

	// Model credentials passed through to the agent container
	AnthropicKey  string
	OpenAIKey     string
	OpenAIBaseURL string
}

// Load resolves configuration from built-in defaults and the environment.
// It performs no network calls; BotUsername is resolved separately at startup.
func Load() *Config {
	// 1. Defaults.
	cfg := &Config{
		RedisURL:       "redis://localhost:6379",
		Trigger:        "aizu",
		ContainerImage: "aizu-agent:pi", // pi-engine sandbox, built via `docker compose build agent`
		EngineCommand:  `pi -p "$(cat {prompt_file})"`,
		Timeout:        3600,
		MaxRunsPerHour: 10,
		PollInterval:   15 * time.Second,
	}

	// 2. Environment overrides.
	if v := os.Getenv("REDIS_URL"); v != "" {
		cfg.RedisURL = v
	}
	if v := os.Getenv("AIZU_TRIGGER"); v != "" {
		cfg.Trigger = v
	}
	if v := envList("AIZU_REPOS"); v != nil {
		cfg.Repos = v
	}
	if v := envList("AIZU_USERS"); v != nil {
		cfg.Users = v
	}
	if v := os.Getenv("AIZU_ALLOW_ALL"); v != "" {
		cfg.AllowAll, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("CONTAINER_IMAGE"); v != "" {
		cfg.ContainerImage = v
	}
	if v := os.Getenv("ENGINE_COMMAND"); v != "" {
		cfg.EngineCommand = v
	}
	if n, ok := envInt("AIZU_TIMEOUT"); ok {
		cfg.Timeout = n
	}
	if n, ok := envInt("AIZU_MAX_RUNS_PER_HOUR"); ok {
		cfg.MaxRunsPerHour = n
	}
	if n, ok := envInt("POLL_INTERVAL"); ok && n > 0 {
		cfg.PollInterval = time.Duration(n) * time.Second
	}

	// Secrets are environment-only.
	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")
	cfg.AnthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.OpenAIBaseURL = v
	}

	return cfg
}

func envList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envInt(key string) (int, bool) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
	}
	return 0, false
}
