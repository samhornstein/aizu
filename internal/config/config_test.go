package config

import (
	"os"
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
	t.Setenv("AIZU_MAX_CONCURRENT", "4")
	t.Setenv("AIZU_REQUESTS_PER_MINUTE", "20")
	t.Setenv("AIZU_RATE_WINDOW_SECONDS", "30")

	cfg := Load()

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
	if cfg.MaxConcurrent != 4 {
		t.Errorf("MaxConcurrent = %d, want 4", cfg.MaxConcurrent)
	}
	if cfg.RequestsPerMinute != 20 {
		t.Errorf("RequestsPerMinute = %d, want 20", cfg.RequestsPerMinute)
	}
	if cfg.RateWindowDuration != 30*time.Second {
		t.Errorf("RateWindowDuration = %v, want 30s", cfg.RateWindowDuration)
	}
}

func TestTOMLOverride(t *testing.T) {
	f, err := os.CreateTemp("", "aizu-test-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	_, err = f.WriteString(`
[trigger]
keyword = "@bot"
repos   = ["owner/repo"]
users   = ["alice"]

[agent]
timeout = 120

[poller]
interval_seconds = 45
`)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("AIZU_CONFIG", f.Name())

	cfg := Load()
	if cfg.Trigger != "@bot" {
		t.Errorf("Trigger = %q, want @bot", cfg.Trigger)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0] != "owner/repo" {
		t.Errorf("Repos = %v, want [owner/repo]", cfg.Repos)
	}
	if len(cfg.Users) != 1 || cfg.Users[0] != "alice" {
		t.Errorf("Users = %v, want [alice]", cfg.Users)
	}
	if cfg.Timeout != 120 {
		t.Errorf("Timeout = %d, want 120", cfg.Timeout)
	}
	if cfg.PollInterval != 45*time.Second {
		t.Errorf("PollInterval = %v, want 45s", cfg.PollInterval)
	}
}

func TestRateLimitDefaults(t *testing.T) {
	cfg := Load()
	if cfg.MaxConcurrent != 1 {
		t.Errorf("MaxConcurrent default = %d, want 1", cfg.MaxConcurrent)
	}
	if cfg.RequestsPerMinute != 0 {
		t.Errorf("RequestsPerMinute default = %d, want 0 (unlimited)", cfg.RequestsPerMinute)
	}
	if cfg.RateWindowDuration != time.Minute {
		t.Errorf("RateWindowDuration default = %v, want 1m", cfg.RateWindowDuration)
	}
}

func TestTOMLRateLimit(t *testing.T) {
	f, err := os.CreateTemp("", "aizu-test-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	_, err = f.WriteString(`
[trigger]
repos = ["owner/repo"]

[ratelimit]
max_concurrent = 5
requests_per_minute = 10
rate_window_seconds = 120
`)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("AIZU_CONFIG", f.Name())

	cfg := Load()
	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", cfg.MaxConcurrent)
	}
	if cfg.RequestsPerMinute != 10 {
		t.Errorf("RequestsPerMinute = %d, want 10", cfg.RequestsPerMinute)
	}
	if cfg.RateWindowDuration != 120*time.Second {
		t.Errorf("RateWindowDuration = %v, want 120s", cfg.RateWindowDuration)
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
