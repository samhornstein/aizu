package executor

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/samhornstein/aizu/internal/config"
)

func TestRunTimesOut(t *testing.T) {
	start := time.Now()
	_, err := run("sleep 5", 100*time.Millisecond)
	if !errors.Is(err, errTimedOut) {
		t.Fatalf("run() error = %v, want errTimedOut", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("run() took %s, want well under the command's 5s sleep", elapsed)
	}
}

func TestRunSucceedsWithinTimeout(t *testing.T) {
	out, err := run("echo hi", time.Second)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if out != "hi\n" {
		t.Errorf("run() output = %q, want %q", out, "hi\n")
	}
}

func TestRunExitErrorIsNotTimeout(t *testing.T) {
	_, err := run("exit 3", time.Second)
	if errors.Is(err, errTimedOut) {
		t.Fatal("run() reported timeout for a plain non-zero exit")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run() error = %v, want *exec.ExitError", err)
	}
}

func TestRunNoTimeout(t *testing.T) {
	out, err := run("echo hi", 0)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if out != "hi\n" {
		t.Errorf("run() output = %q, want %q", out, "hi\n")
	}
}

func TestSandboxURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1":            "http://host.docker.internal:11434/v1",
		"http://127.0.0.1:8080/v1":             "http://host.docker.internal:8080/v1",
		"http://host.docker.internal:11434/v1": "http://host.docker.internal:11434/v1",
		"https://api.example.com/v1":           "https://api.example.com/v1",
		"":                                     "",
	}
	for in, want := range cases {
		if got := sandboxURL(in); got != want {
			t.Errorf("sandboxURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"", "''"},
		{"it's", "'it'\"'\"'s'"},
		{"say 'hi' there", "'say '\"'\"'hi'\"'\"' there'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShellQuoteRoundTrip(t *testing.T) {
	// Verify that shellQuote output is safe to pass to sh -c.
	inputs := []string{"hello", "it's fine", "a & b | c", "$(rm -rf /)"}
	for _, in := range inputs {
		quoted := shellQuote(in)
		// Quoted string must start and end with a single quote.
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Errorf("shellQuote(%q) = %q: not single-quoted", in, quoted)
		}
	}
}

func TestEnvExportsAlwaysIncludesGitIdentity(t *testing.T) {
	cfg := &config.Config{}
	out := envExports(cfg)
	for _, want := range []string{
		"GIT_AUTHOR_NAME=aizu",
		"GIT_AUTHOR_EMAIL=aizu@noreply",
		"GIT_COMMITTER_NAME=aizu",
		"GIT_COMMITTER_EMAIL=aizu@noreply",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("envExports missing %q; got: %s", want, out)
		}
	}
}

func TestEnvExportsIncludesSetKeys(t *testing.T) {
	cfg := &config.Config{
		AnthropicKey: "sk-ant",
		OpenAIKey:    "sk-oai",
		GitHubToken:  "ghp_test",
	}
	out := envExports(cfg)
	for _, want := range []string{"ANTHROPIC_API_KEY=", "OPENAI_API_KEY=", "GITHUB_TOKEN=", "GH_TOKEN="} {
		if !strings.Contains(out, want) {
			t.Errorf("envExports missing %q; got: %s", want, out)
		}
	}
}

func TestEnvExportsOmitsEmptyKeys(t *testing.T) {
	cfg := &config.Config{} // all keys empty
	out := envExports(cfg)
	for _, absent := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GITHUB_TOKEN", "GH_TOKEN", "MODEL_SERVER_HOST"} {
		if strings.Contains(out, absent) {
			t.Errorf("envExports should omit unset key %q; got: %s", absent, out)
		}
	}
}
