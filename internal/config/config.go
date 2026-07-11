// Package config loads Aizu's configuration from, in increasing order of
// precedence: built-in defaults and environment variables.
package config

import (
	"log/slog"
	"os"
	"sort"
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
	Engine             string // preset name: pi | claude (see enginePresets)
	ContainerImage     string // image the agent runs in
	EngineCommand      string // command run inside the container; {prompt_file} is substituted
	EngineLocalCommand string // variant used when a local model server is configured; {model} is substituted. Empty = always use EngineCommand.
	Timeout            int    // agent run timeout, seconds
	MaxRunsPerHour     int    // per-repo hourly cap on agent runs; 0 disables

	// Poller
	PollInterval time.Duration

	// Model credentials passed through to the agent container
	AnthropicKey  string
	OpenAIKey     string
	OpenAIBaseURL string
}

// enginePreset bundles the sandbox image and run commands for a known agent.
// LocalCommand is used instead of Command when a local model server is
// configured (OPENAI_BASE_URL set or auto-detected); it may contain {model},
// substituted at run time with the server's discovered model ID. Engines
// without a LocalCommand run Command regardless.
type enginePreset struct {
	Image        string
	Command      string
	LocalCommand string
}

var enginePresets = map[string]enginePreset{
	"pi": {
		Image:        "ghcr.io/samhornstein/aizu-agent-pi:latest",
		Command:      `pi -p "$(cat {prompt_file})"`,
		LocalCommand: `pi --model {model} -p "$(cat {prompt_file})"`,
	},
	"claude": {
		Image:   "ghcr.io/samhornstein/aizu-agent-claude:latest",
		Command: `claude --dangerously-skip-permissions -p "$(cat {prompt_file})"`,
	},
}

// Load resolves configuration from built-in defaults and the environment.
// It performs no network calls; BotUsername is resolved separately at startup.
func Load() *Config {
	// 1. Defaults.
	cfg := &Config{
		RedisURL:       "redis://localhost:6379",
		Trigger:        "aizu",
		Engine:         "pi",
		Timeout:        3600,
		MaxRunsPerHour: 10,
		PollInterval:   15 * time.Second,
	}

	// 2. Engine preset — resolved before the CONTAINER_IMAGE/ENGINE_COMMAND
	// overrides below, so those remain expert-level overrides of a preset.
	if v := os.Getenv("AIZU_ENGINE"); v != "" {
		if _, ok := enginePresets[v]; ok {
			cfg.Engine = v
		} else {
			slog.Warn("Unknown AIZU_ENGINE; falling back to pi", "engine", v, "valid", presetNames())
		}
	}
	preset := enginePresets[cfg.Engine]
	cfg.ContainerImage = preset.Image
	cfg.EngineCommand = preset.Command
	cfg.EngineLocalCommand = preset.LocalCommand

	// 3. Environment overrides.
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
		// An explicit command wins everywhere; clear the local variant so it
		// runs unmodified with or without a local model server (it may still
		// carry {model} if the user wants substitution).
		cfg.EngineCommand = v
		cfg.EngineLocalCommand = ""
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

func presetNames() []string {
	names := make([]string, 0, len(enginePresets))
	for name := range enginePresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
