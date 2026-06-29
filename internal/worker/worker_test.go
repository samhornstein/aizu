package worker

import (
	"errors"
	"strings"
	"testing"

	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
)

// mockExecutor satisfies executor.Executor with a controllable ReadFile response.
type mockExecutor struct {
	fileContent string
	fileErr     error
}

func (m *mockExecutor) Create(repo, branch string) (string, error) { return "sid-test", nil }
func (m *mockExecutor) RunEngine(sid, prompt string) (int, string, error) {
	return 0, "", nil
}
func (m *mockExecutor) ReadFile(sid, path string) (string, error) {
	return m.fileContent, m.fileErr
}
func (m *mockExecutor) Destroy(sid string) {}
func (m *mockExecutor) CleanupStale()      {}

func newTestWorker(exec *mockExecutor, defaultInstructions string) *Worker {
	return &Worker{
		exec:   exec,
		loader: template.NewLoader(defaultInstructions),
	}
}

func TestFormatResultTrimmed(t *testing.T) {
	if got := formatResult("  hello  "); got != "hello" {
		t.Errorf("formatResult = %q, want hello", got)
	}
}

func TestFormatResultEmpty(t *testing.T) {
	if got := formatResult("   "); got != "Done." {
		t.Errorf("formatResult(empty) = %q, want Done.", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want hello", got)
	}
	long := strings.Repeat("a", 20)
	got := truncate(long, 10)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("truncate long = %q, want truncated suffix", got)
	}
	if len(got) > 10+len("\n…(truncated)") {
		t.Errorf("truncate long too long: %d chars", len(got))
	}
}

func TestBuildPromptContainsContext(t *testing.T) {
	w := newTestWorker(&mockExecutor{fileErr: errors.New("no file")}, "default system prompt")

	issue := &github.Issue{Number: 42, Title: "Fix the bug", Body: "It crashes on startup."}
	task := &queue.Task{Author: "alice", Body: "@aizu please fix", Repo: "owner/repo", Number: 42}

	prompt := w.buildPrompt("sid-1", issue, task)

	for _, want := range []string{
		"default system prompt",
		"#42",
		"Fix the bug",
		"It crashes on startup.",
		"@alice",
		"@aizu please fix",
		"owner/repo",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("buildPrompt missing %q", want)
		}
	}
}

func TestBuildPromptUsesRepoInstructions(t *testing.T) {
	w := newTestWorker(&mockExecutor{fileContent: "repo-specific instructions"}, "default")

	issue := &github.Issue{Number: 1, Title: "T"}
	task := &queue.Task{Author: "alice", Body: "@aizu", Repo: "owner/repo", Number: 1}

	prompt := w.buildPrompt("sid-1", issue, task)

	if !strings.Contains(prompt, "repo-specific instructions") {
		t.Error("buildPrompt should use repo instructions when available")
	}
	if strings.Contains(prompt, "default") {
		t.Error("buildPrompt should not include default instructions when repo file is present")
	}
}

func TestBuildPromptIssueVsPR(t *testing.T) {
	w := newTestWorker(&mockExecutor{fileErr: errors.New("no file")}, "sys")

	prIssue := &github.Issue{
		Number: 7,
		Title:  "My PR",
		PullRequest: &struct {
			URL string `json:"url"`
		}{URL: "x"},
	}
	task := &queue.Task{Author: "bob", Body: "@aizu", Repo: "owner/repo", Number: 7}

	prompt := w.buildPrompt("sid-1", prIssue, task)
	if !strings.Contains(prompt, "pull request") {
		t.Errorf("buildPrompt for PR should say 'pull request'; got: %s", prompt)
	}

	regIssue := &github.Issue{Number: 8, Title: "Bug"}
	task.Number = 8
	prompt = w.buildPrompt("sid-1", regIssue, task)
	if !strings.Contains(prompt, "issue") {
		t.Errorf("buildPrompt for issue should say 'issue'; got: %s", prompt)
	}
}
