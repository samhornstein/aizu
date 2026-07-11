package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

	// Recorded by Create/RunEngine for assertions.
	gotBranch   string
	gotPRNumber int
	gotPrompt   string
}

func (m *mockExecutor) Create(repo, branch string, prNumber int) (string, error) {
	m.gotBranch = branch
	m.gotPRNumber = prNumber
	return "sid-test", nil
}
func (m *mockExecutor) RunEngine(sid, prompt string) (int, string, error) {
	m.gotPrompt = prompt
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
	task := &queue.Task{Author: "alice", Body: "@aizu please fix", Repo: "owner/repo", Number: 42, CommentID: 999}

	prompt := w.buildPrompt("sid-1", issue, task, false)

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
	task := &queue.Task{Author: "alice", Body: "@aizu", Repo: "owner/repo", Number: 1, CommentID: 1}

	prompt := w.buildPrompt("sid-1", issue, task, false)

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
	task := &queue.Task{Author: "bob", Body: "@aizu", Repo: "owner/repo", Number: 7, CommentID: 1}

	prompt := w.buildPrompt("sid-1", prIssue, task, false)
	if !strings.Contains(prompt, "pull request") {
		t.Errorf("buildPrompt for PR should say 'pull request'; got: %s", prompt)
	}

	regIssue := &github.Issue{Number: 8, Title: "Bug"}
	task.Number = 8
	prompt = w.buildPrompt("sid-1", regIssue, task, false)
	if !strings.Contains(prompt, "issue") {
		t.Errorf("buildPrompt for issue should say 'issue'; got: %s", prompt)
	}
}

func TestBuildPromptForkNote(t *testing.T) {
	w := newTestWorker(&mockExecutor{fileErr: errors.New("no file")}, "sys")

	prIssue := &github.Issue{
		Number: 7,
		Title:  "Fork PR",
		PullRequest: &struct {
			URL string `json:"url"`
		}{URL: "x"},
	}
	task := &queue.Task{Author: "bob", Body: "@aizu", Repo: "owner/repo", Number: 7, CommentID: 1}

	fork := w.buildPrompt("sid-1", prIssue, task, true)
	if !strings.Contains(fork, "comes from a fork") {
		t.Errorf("fork PR prompt missing fork note; got: %s", fork)
	}

	sameRepo := w.buildPrompt("sid-1", prIssue, task, false)
	if strings.Contains(sameRepo, "comes from a fork") {
		t.Error("same-repo PR prompt must not carry the fork note")
	}

	// isFork is meaningless for plain issues — never add the note.
	regIssue := &github.Issue{Number: 8, Title: "Bug"}
	if got := w.buildPrompt("sid-1", regIssue, task, true); strings.Contains(got, "comes from a fork") {
		t.Error("issue prompt must not carry the fork note")
	}
}

// TestHandleThreadsPRNumber drives handle() against a fake GitHub server and
// asserts the executor receives the PR number and head branch, and that a
// fork PR's prompt carries the fork note.
func TestHandleThreadsPRNumber(t *testing.T) {
	cases := []struct {
		name         string
		headRepo     string
		wantForkNote bool
	}{
		{"same-repo PR", "o/r", false},
		{"fork PR", "someone/fork", true},
		{"deleted fork (head.repo null)", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/o/r/issues/5", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"number": 5, "title": "A PR",
					"pull_request": map[string]string{"url": "x"},
				})
			})
			mux.HandleFunc("/repos/o/r/pulls/5", func(w http.ResponseWriter, r *http.Request) {
				head := map[string]any{"ref": "feature"}
				if tc.headRepo != "" {
					head["repo"] = map[string]string{"full_name": tc.headRepo}
				} else {
					head["repo"] = nil
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"number": 5, "head": head})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			exec := &mockExecutor{fileErr: errors.New("no file")}
			w := &Worker{
				exec:   exec,
				gh:     github.NewWithBaseURL("t", srv.URL),
				loader: template.NewLoader("sys"),
			}
			task := &queue.Task{Repo: "o/r", Number: 5, CommentID: 1, Author: "bob", Body: "@aizu"}

			if _, err := w.handle(context.Background(), task, slog.Default()); err != nil {
				t.Fatalf("handle() error = %v", err)
			}
			if exec.gotPRNumber != 5 {
				t.Errorf("executor got prNumber %d, want 5", exec.gotPRNumber)
			}
			if exec.gotBranch != "feature" {
				t.Errorf("executor got branch %q, want feature", exec.gotBranch)
			}
			hasNote := strings.Contains(exec.gotPrompt, "comes from a fork")
			if hasNote != tc.wantForkNote {
				t.Errorf("fork note present = %v, want %v", hasNote, tc.wantForkNote)
			}
		})
	}
}

func TestBuildPromptIssueBodyTrigger(t *testing.T) {
	w := newTestWorker(&mockExecutor{fileErr: errors.New("no file")}, "sys")

	issue := &github.Issue{
		Number: 25,
		Title:  "Add feature",
		Body:   "@aizu implement this feature please",
	}
	// CommentID=0 signals an issue-body trigger (no comment).
	task := &queue.Task{Author: "alice", Body: "@aizu", Repo: "owner/repo", Number: 25, CommentID: 0}

	prompt := w.buildPrompt("sid-1", issue, task, false)

	// Should reference the issue body as the trigger, not a comment.
	if strings.Contains(prompt, "responding to a comment") {
		t.Error("issue-body trigger should not say 'responding to a comment'")
	}
	if !strings.Contains(prompt, "responding to issue") {
		t.Errorf("should say 'responding to issue'; got: %s", prompt)
	}
	if !strings.Contains(prompt, "@aizu implement this feature please") {
		t.Error("should include the issue body as the request")
	}
	if !strings.Contains(prompt, "in the issue body") {
		t.Error("should mention 'in the issue body'")
	}
}
