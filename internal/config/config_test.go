package config

import (
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
