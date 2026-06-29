package executor

import (
	"strings"
	"testing"

	"github.com/samhornstein/aizu/internal/config"
)

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
		AnthropicKey:    "sk-ant",
		OpenAIKey:       "sk-oai",
		ModelServerHost: "localhost:8080",
	}
	out := envExports(cfg)
	for _, want := range []string{"ANTHROPIC_API_KEY=", "OPENAI_API_KEY=", "MODEL_SERVER_HOST="} {
		if !strings.Contains(out, want) {
			t.Errorf("envExports missing %q; got: %s", want, out)
		}
	}
}

func TestEnvExportsOmitsEmptyKeys(t *testing.T) {
	cfg := &config.Config{} // all keys empty
	out := envExports(cfg)
	for _, absent := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "MODEL_SERVER_HOST"} {
		if strings.Contains(out, absent) {
			t.Errorf("envExports should omit unset key %q; got: %s", absent, out)
		}
	}
}
