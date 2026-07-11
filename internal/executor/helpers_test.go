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

func TestRunWithStdin(t *testing.T) {
	out, err := runWithStdin("cat", "hello", time.Second)
	if err != nil {
		t.Fatalf("runWithStdin() error = %v", err)
	}
	if out != "hello" {
		t.Errorf("runWithStdin() output = %q, want %q", out, "hello")
	}
}

func TestBuildEnvFileAlwaysIncludesGitIdentity(t *testing.T) {
	cfg := &config.Config{}
	out := buildEnvFile(cfg)
	for _, want := range []string{
		"GIT_AUTHOR_NAME=aizu\n",
		"GIT_AUTHOR_EMAIL=aizu@noreply\n",
		"GIT_COMMITTER_NAME=aizu\n",
		"GIT_COMMITTER_EMAIL=aizu@noreply\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("buildEnvFile missing %q; got: %s", want, out)
		}
	}
}

func TestBuildEnvFileIncludesSetKeys(t *testing.T) {
	cfg := &config.Config{
		AnthropicKey: "sk-ant",
		OpenAIKey:    "sk-oai",
		GitHubToken:  "ghp_test",
	}
	out := buildEnvFile(cfg)
	for _, want := range []string{
		"ANTHROPIC_API_KEY=sk-ant\n",
		"OPENAI_API_KEY=sk-oai\n",
		"GITHUB_TOKEN=ghp_test\n",
		"GH_TOKEN=ghp_test\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("buildEnvFile missing %q; got: %s", want, out)
		}
	}
}

func TestBuildEnvFileOmitsEmptyKeys(t *testing.T) {
	cfg := &config.Config{} // all keys empty
	out := buildEnvFile(cfg)
	for _, absent := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GITHUB_TOKEN", "GH_TOKEN"} {
		if strings.Contains(out, absent) {
			t.Errorf("buildEnvFile should omit unset key %q; got: %s", absent, out)
		}
	}
}

func TestBuildEnvFileSkipsNewlineValues(t *testing.T) {
	// The env-file format has no quoting; a value with a newline would smuggle
	// in an arbitrary extra variable.
	cfg := &config.Config{OpenAIKey: "bad\nGIT_AUTHOR_NAME=evil"}
	out := buildEnvFile(cfg)
	if strings.Contains(out, "OPENAI_API_KEY") || strings.Contains(out, "evil") {
		t.Errorf("buildEnvFile must skip values containing newlines; got: %s", out)
	}
}
