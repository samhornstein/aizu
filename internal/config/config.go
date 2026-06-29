// Package config loads Aizu's configuration from, in increasing order of
// precedence: built-in defaults, an aizu.toml file, and environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// Queue
	RedisURL string

	// Trigger
	Trigger string   // keyword that fires the agent, e.g. "@aizu"
	Repos   []string // "owner/repo" list; empty means auto-discover the token owner's repos
	Users   []string // allowlisted comment authors; empty means allow everyone

	// GitHub
	GitHubToken string // personal access token (PAT)
	BotUsername string // authenticated account login; comments from it are ignored (set at startup)

	// Agent
	ContainerImage string // image the agent runs in
	EngineCommand  string // command run inside the container; {prompt_file} is substituted
	Timeout        int    // agent run timeout, seconds

	// Poller
	PollInterval time.Duration

	// Model credentials passed through to the agent container
	AnthropicKey    string
	OpenAIKey       string
	ModelServerHost string
}

type tomlConfig struct {
	Queue struct {
		RedisURL string `toml:"redis_url"`
	} `toml:"queue"`
	Trigger struct {
		Keyword string   `toml:"keyword"`
		Repos   []string `toml:"repos"`
		Users   []string `toml:"users"`
	} `toml:"trigger"`
	Agent struct {
		Image   string `toml:"image"`
		Command string `toml:"command"`
		Timeout int    `toml:"timeout"`
	} `toml:"agent"`
	Models struct {
		ModelServerHost string `toml:"model_server_host"`
	} `toml:"models"`
	Poller struct {
		IntervalSeconds int `toml:"interval_seconds"`
	} `toml:"poller"`
}

// Load resolves configuration from defaults, aizu.toml, and the environment.
// It performs no network calls; BotUsername is resolved separately at startup.
func Load() *Config {
	// 1. Defaults.
	cfg := &Config{
		RedisURL:       "redis://localhost:6379",
		Trigger:        "@aizu",
		ContainerImage: "ghcr.io/samhornstein/aizu-agent:pi",
		EngineCommand:  `pi -p "$(cat {prompt_file})"`,
		Timeout:        600,
		PollInterval:   30 * time.Second,
	}

	// 2. aizu.toml, if present in the working directory (override AIZU_CONFIG).
	path := "aizu.toml"
	if v := os.Getenv("AIZU_CONFIG"); v != "" {
		path = v
	}
	var tc tomlConfig
	if _, err := toml.DecodeFile(path, &tc); err == nil {
		if tc.Queue.RedisURL != "" {
			cfg.RedisURL = tc.Queue.RedisURL
		}
		if tc.Trigger.Keyword != "" {
			cfg.Trigger = tc.Trigger.Keyword
		}
		if len(tc.Trigger.Repos) > 0 {
			cfg.Repos = tc.Trigger.Repos
		}
		if len(tc.Trigger.Users) > 0 {
			cfg.Users = tc.Trigger.Users
		}
		if tc.Agent.Image != "" {
			cfg.ContainerImage = tc.Agent.Image
		}
		if tc.Agent.Command != "" {
			cfg.EngineCommand = tc.Agent.Command
		}
		if tc.Agent.Timeout != 0 {
			cfg.Timeout = tc.Agent.Timeout
		}
		if tc.Models.ModelServerHost != "" {
			cfg.ModelServerHost = tc.Models.ModelServerHost
		}
		if tc.Poller.IntervalSeconds > 0 {
			cfg.PollInterval = time.Duration(tc.Poller.IntervalSeconds) * time.Second
		}
	}

	// 3. Environment overrides (highest precedence).
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
	if v := os.Getenv("CONTAINER_IMAGE"); v != "" {
		cfg.ContainerImage = v
	}
	if v := os.Getenv("ENGINE_COMMAND"); v != "" {
		cfg.EngineCommand = v
	}
	if n, ok := envInt("AIZU_TIMEOUT"); ok {
		cfg.Timeout = n
	}
	if n, ok := envInt("POLL_INTERVAL"); ok && n > 0 {
		cfg.PollInterval = time.Duration(n) * time.Second
	}

	// Secrets are environment-only.
	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")
	cfg.AnthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	if v := os.Getenv("MODEL_SERVER_HOST"); v != "" {
		cfg.ModelServerHost = v
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
