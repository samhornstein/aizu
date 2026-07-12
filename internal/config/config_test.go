package config

import (
	"strings"
	"testing"
	"time"
)

func TestEnvOverrides(t *testing.T) {
	t.Setenv("AIZU_TRIGGER", "@bot")
	t.Setenv("AIZU_REPOS", "owner/repo1, owner/repo2")
	t.Setenv("AIZU_USERS", "alice, bob")
	t.Setenv("POLL_INTERVAL", "60")
	t.Setenv("AIZU_TIMEOUT", "300")
	t.Setenv("REDIS_URL", "redis://myhost:6379")
	t.Setenv("CONTAINER_IMAGE", "my-agent:dev")

	cfg := Load()

	if cfg.ContainerImage != "my-agent:dev" {
		t.Errorf("ContainerImage = %q, want my-agent:dev (env must override the ghcr default)", cfg.ContainerImage)
	}

	if cfg.Trigger != "@bot" {
		t.Errorf("Trigger = %q, want @bot", cfg.Trigger)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[0] != "owner/repo1" || cfg.Repos[1] != "owner/repo2" {
		t.Errorf("Repos = %v, want [owner/repo1 owner/repo2]", cfg.Repos)
	}
	if len(cfg.Users) != 2 || cfg.Users[0] != "alice" || cfg.Users[1] != "bob" {
		t.Errorf("Users = %v, want [alice bob]", cfg.Users)
	}
	if cfg.PollInterval != 60*time.Second {
		t.Errorf("PollInterval = %v, want 60s", cfg.PollInterval)
	}
	if cfg.Timeout != 300 {
		t.Errorf("Timeout = %d, want 300", cfg.Timeout)
	}
	if cfg.RedisURL != "redis://myhost:6379" {
		t.Errorf("RedisURL = %q, want redis://myhost:6379", cfg.RedisURL)
	}
}

func TestDefaults(t *testing.T) {
	cfg := Load()

	if cfg.Trigger != "aizu" {
		t.Errorf("Trigger = %q, want aizu", cfg.Trigger)
	}
	if cfg.ContainerImage != "ghcr.io/samhornstein/aizu-agent-pi:latest" {
		t.Errorf("ContainerImage = %q, want the ghcr.io agent image", cfg.ContainerImage)
	}
	if cfg.Timeout != 3600 {
		t.Errorf("Timeout = %d, want 3600", cfg.Timeout)
	}
	if cfg.PollInterval != 15*time.Second {
		t.Errorf("PollInterval = %v, want 15s", cfg.PollInterval)
	}
	if !cfg.Signature {
		t.Error("Signature = false, want true by default")
	}
}

func TestSignatureOverride(t *testing.T) {
	t.Setenv("AIZU_SIGNATURE", "false")
	if Load().Signature {
		t.Error("Signature = true, want false with AIZU_SIGNATURE=false")
	}

	t.Setenv("AIZU_SIGNATURE", "true")
	if !Load().Signature {
		t.Error("Signature = false, want true with AIZU_SIGNATURE=true")
	}
}

func TestConcurrencyDefault(t *testing.T) {
	if got := Load().Concurrency; got != 1 {
		t.Errorf("Concurrency = %d, want 1", got)
	}
}

func TestConcurrencyOverride(t *testing.T) {
	t.Setenv("AIZU_CONCURRENCY", "4")
	if got := Load().Concurrency; got != 4 {
		t.Errorf("Concurrency = %d, want 4", got)
	}
}

func TestConcurrencyInvalidIgnored(t *testing.T) {
	for _, bad := range []string{"0", "-1", "lots"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AIZU_CONCURRENCY", bad)
			if got := Load().Concurrency; got != 1 {
				t.Errorf("Concurrency = %d, want 1 (invalid %q ignored)", got, bad)
			}
		})
	}
}

func TestEngineDefaultIsPi(t *testing.T) {
	cfg := Load()
	if cfg.Engine != "pi" {
		t.Errorf("Engine = %q, want pi", cfg.Engine)
	}
	if cfg.EngineLocalCommand == "" {
		t.Error("pi preset should carry a local-command variant")
	}
}

func TestEnginePresetClaude(t *testing.T) {
	t.Setenv("AIZU_ENGINE", "claude")
	cfg := Load()
	if cfg.Engine != "claude" {
		t.Errorf("Engine = %q, want claude", cfg.Engine)
	}
	if cfg.ContainerImage != "ghcr.io/samhornstein/aizu-agent-claude:latest" {
		t.Errorf("ContainerImage = %q, want the claude ghcr image", cfg.ContainerImage)
	}
	if !strings.Contains(cfg.EngineCommand, "claude") {
		t.Errorf("EngineCommand = %q, want the claude command", cfg.EngineCommand)
	}
	if cfg.EngineLocalCommand != "" {
		t.Errorf("EngineLocalCommand = %q, want empty (claude has no local variant)", cfg.EngineLocalCommand)
	}
}

func TestEnginePresetAider(t *testing.T) {
	t.Setenv("AIZU_ENGINE", "aider")
	cfg := Load()
	if cfg.ContainerImage != "ghcr.io/samhornstein/aizu-agent-aider:latest" {
		t.Errorf("ContainerImage = %q, want the aider ghcr image", cfg.ContainerImage)
	}
	if !strings.Contains(cfg.EngineCommand, "aider --yes-always") {
		t.Errorf("EngineCommand = %q, want a non-interactive aider command", cfg.EngineCommand)
	}
	for _, ph := range []string{"{model}", "{base_url}"} {
		if !strings.Contains(cfg.EngineLocalCommand, ph) {
			t.Errorf("EngineLocalCommand = %q, want it to carry %s", cfg.EngineLocalCommand, ph)
		}
	}
}

func TestEnginePresetOpencode(t *testing.T) {
	t.Setenv("AIZU_ENGINE", "opencode")
	cfg := Load()
	if cfg.ContainerImage != "ghcr.io/samhornstein/aizu-agent-opencode:latest" {
		t.Errorf("ContainerImage = %q, want the opencode ghcr image", cfg.ContainerImage)
	}
	if !strings.Contains(cfg.EngineCommand, "opencode run") {
		t.Errorf("EngineCommand = %q, want the opencode run command", cfg.EngineCommand)
	}
	if cfg.EngineLocalCommand != "" {
		t.Errorf("EngineLocalCommand = %q, want empty (opencode has no local variant)", cfg.EngineLocalCommand)
	}
}

func TestEngineUnknownFallsBackToPi(t *testing.T) {
	t.Setenv("AIZU_ENGINE", "notreal")
	cfg := Load()
	if cfg.Engine != "pi" {
		t.Errorf("Engine = %q, want pi fallback", cfg.Engine)
	}
	if cfg.ContainerImage != "ghcr.io/samhornstein/aizu-agent-pi:latest" {
		t.Errorf("ContainerImage = %q, want the pi image", cfg.ContainerImage)
	}
}

func TestEngineCommandOverridesPreset(t *testing.T) {
	t.Setenv("AIZU_ENGINE", "claude")
	t.Setenv("ENGINE_COMMAND", "my-agent {prompt_file}")
	cfg := Load()
	if cfg.EngineCommand != "my-agent {prompt_file}" {
		t.Errorf("EngineCommand = %q, want the explicit override", cfg.EngineCommand)
	}
	if cfg.EngineLocalCommand != "" {
		t.Error("explicit ENGINE_COMMAND must clear the preset's local variant")
	}
}

func TestEnvListEmpty(t *testing.T) {
	if got := envList("AIZU_TEST_NONEXISTENT_VAR"); got != nil {
		t.Errorf("envList(missing) = %v, want nil", got)
	}
}

func TestEnvIntInvalid(t *testing.T) {
	t.Setenv("AIZU_TEST_BAD_INT", "notanumber")
	if _, ok := envInt("AIZU_TEST_BAD_INT"); ok {
		t.Error("envInt(non-numeric) should return ok=false")
	}
}
