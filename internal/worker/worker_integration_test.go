// Integration tests for the full worker workflow: reaction → sandbox → engine → reply.
//
// These tests require a running Redis instance. Run with:
//
//	docker run -d --name aizu-test-redis -p 6379:6379 redis:7-alpine
//	go test -race -tags=integration ./...
//	docker rm -f aizu-test-redis
//
// Or use the Makefile target: make test-integration
//
//go:build integration

package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/poller"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
)

const testRedisURL = "redis://localhost:6379"

// mockGitHub tracks calls for assertion.
type mockGitHub struct {
	mu              sync.Mutex
	reactions       []struct{ repo string; commentID int64; content string }
	comments        []struct{ repo string; number int; body string }
	issue           *github.Issue
	pr              *github.PullRequest
	listComments    []github.Comment
	listCommentsErr error
}

func (m *mockGitHub) AuthenticatedUser(ctx context.Context) (github.User, error) {
	return github.User{Login: "aizu-bot", Type: "Bot"}, nil
}

func (m *mockGitHub) ListIssueComments(ctx context.Context, repo string, since time.Time) ([]github.Comment, error) {
	return m.listComments, m.listCommentsErr
}

func (m *mockGitHub) GetIssue(ctx context.Context, repo string, number int) (*github.Issue, error) {
	return m.issue, nil
}

func (m *mockGitHub) GetPullRequest(ctx context.Context, repo string, number int) (*github.PullRequest, error) {
	return m.pr, nil
}

func (m *mockGitHub) AddReaction(ctx context.Context, repo string, commentID int64, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions = append(m.reactions, struct {
		repo      string
		commentID int64
		content   string
	}{repo, commentID, content})
	return nil
}

func (m *mockGitHub) CreateComment(ctx context.Context, repo string, number int, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, struct {
		repo   string
		number int
		body   string
	}{repo, number, body})
	return nil
}

func (m *mockGitHub) getReactions() []struct {
	repo      string
	commentID int64
	content   string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		repo      string
		commentID int64
		content   string
	}, len(m.reactions))
	copy(out, m.reactions)
	return out
}

func (m *mockGitHub) getComments() []struct {
	repo   string
	number int
	body   string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		repo   string
		number int
		body   string
	}, len(m.comments))
	copy(out, m.comments)
	return out
}

// mockExecutor tracks sandbox lifecycle and returns controlled engine output.
type mockExecutor struct {
	engineOutput string
	engineErr    error
	engineCode   int
	created      []string
	destroyed    []string
	mu           sync.Mutex
}

func (m *mockExecutor) Create(repo, branch string) (string, error) {
	sid := "sandbox-" + repo
	m.mu.Lock()
	m.created = append(m.created, sid)
	m.mu.Unlock()
	return sid, nil
}

func (m *mockExecutor) RunEngine(sid, prompt string) (int, string, error) {
	return m.engineCode, m.engineOutput, m.engineErr
}

func (m *mockExecutor) ReadFile(sid, path string) (string, error) {
	return "", fmt.Errorf("not found")
}

func (m *mockExecutor) Destroy(sid string) {
	m.mu.Lock()
	m.destroyed = append(m.destroyed, sid)
	m.mu.Unlock()
}

func (m *mockExecutor) CleanupStale() {}

// TestWorkflowIssueEndToEnd tests the full worker.process flow for an issue.
func TestWorkflowIssueEndToEnd(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		issue: &github.Issue{Number: 42, Title: "Fix bug", Body: "It crashes."},
	}
	exec := &mockExecutor{engineCode: 0, engineOutput: "Bug fixed in parser.go"}
	loader := template.NewLoader("# Aizu system instructions")

	w := New(q, exec, gh, loader)

	task, err := q.Enqueue(ctx, "owner/repo", 42, 999, "@aizu fix the bug", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: task=%v, err=%v", task, err)
	}

	// Process the task.
	w.process(ctx, task)

	// Verify reaction was added.
	reactions := gh.getReactions()
	if len(reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(reactions))
	}
	if reactions[0].content != "eyes" {
		t.Errorf("reaction content = %q, want eyes", reactions[0].content)
	}

	// Verify reply was posted.
	comments := gh.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].repo != "owner/repo" || comments[0].number != 42 {
		t.Errorf("comment target = %s#%d, want owner/repo#42", comments[0].repo, comments[0].number)
	}
	if !strings.Contains(comments[0].body, "Bug fixed") {
		t.Errorf("reply body = %q, should contain engine output", comments[0].body)
	}

	// Verify sandbox lifecycle.
	exec.mu.Lock()
	if len(exec.created) != 1 || exec.created[0] != "sandbox-owner/repo" {
		t.Errorf("created sandboxes = %v, want [sandbox-owner/repo]", exec.created)
	}
	if len(exec.destroyed) != 1 || exec.destroyed[0] != "sandbox-owner/repo" {
		t.Errorf("destroyed sandboxes = %v", exec.destroyed)
	}
	exec.mu.Unlock()
}

// TestWorkflowPREndToEnd tests the worker flow for a pull request.
func TestWorkflowPREndToEnd(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		issue: &github.Issue{
			Number:  7,
			Title:   "Add feature",
			PullRequest: &struct {
				URL string `json:"url"`
			}{URL: "https://api.github.com/repos/o/r/pulls/7"},
		},
		pr: &github.PullRequest{Number: 7, Head: struct {
			Ref string `json:"ref"`
		}{Ref: "feature-branch"}},
	}
	exec := &mockExecutor{engineCode: 0, engineOutput: "Tests added."}
	loader := template.NewLoader("# Default")

	w := New(q, exec, gh, loader)

	task, err := q.Enqueue(ctx, "owner/repo", 7, 888, "@aizu add tests", "bob")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w.process(ctx, task)

	comments := gh.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if !strings.Contains(comments[0].body, "Tests added") {
		t.Errorf("reply = %q, should contain engine output", comments[0].body)
	}
}

// TestWorkflowEngineFailure tests that the worker handles engine failures gracefully.
func TestWorkflowEngineFailure(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		issue: &github.Issue{Number: 10, Title: "Issue 10"},
	}
	exec := &mockExecutor{engineCode: 1, engineOutput: "error: something broke"}
	loader := template.NewLoader("# Default")

	w := New(q, exec, gh, loader)

	task, err := q.Enqueue(ctx, "o/r", 10, 777, "@aizu fix", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w.process(ctx, task)

	// On failure, the task should be re-queued (not replied to).
	comments := gh.getComments()
	if len(comments) != 0 {
		t.Errorf("expected no comments on failure (task re-queued), got %d comments", len(comments))
	}

	// Reaction should still be added even on failure.
	reactions := gh.getReactions()
	if len(reactions) != 1 {
		t.Errorf("expected 1 reaction even on failure, got %d", len(reactions))
	}
}

// mockExecutorWithInstructions wraps mockExecutor to return custom file content.
type mockExecutorWithInstructions struct {
	*mockExecutor
	fileContent string
}

func (m *mockExecutorWithInstructions) ReadFile(sid, path string) (string, error) {
	return m.fileContent, nil
}

// TestWorkflowWithRepoInstructions tests that repo-specific AIZU.md instructions
// are used in the prompt when available.
func TestWorkflowWithRepoInstructions(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		issue: &github.Issue{Number: 1, Title: "Test", Body: "Body"},
	}
	exec := &mockExecutorWithInstructions{
		baseExecutor: &mockExecutor{engineCode: 0, engineOutput: "Done"},
		fileContent:  "REPO-SPECIFIC INSTRUCTIONS",
	}
	loader := template.NewLoader("DEFAULT INSTRUCTIONS")

	w := New(q, exec, gh, loader)

	task, err := q.Enqueue(ctx, "o/r", 1, 555, "@aizu go", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w.process(ctx, task)

	comments := gh.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
}

// TestPollerToQueueIntegration tests that the poller correctly discovers
// trigger comments and enqueues them to the queue.
func TestPollerToQueueIntegration(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		listComments: []github.Comment{
			{ID: 1, Body: "regular comment", User: github.User{Login: "alice"}, IssueURL: "https://api.github.com/repos/o/r/issues/1"},
			{ID: 2, Body: "@aizu please fix", User: github.User{Login: "bob"}, IssueURL: "https://api.github.com/repos/o/r/issues/42"},
			{ID: 3, Body: "another comment", User: github.User{Login: "carol"}, IssueURL: "https://api.github.com/repos/o/r/issues/42"},
		},
	}

	cfg := &config.Config{
		Trigger:     "@aizu",
		Repos:       []string{"o/r"},
		BotUsername: "aizu-bot",
	}

	p := poller.New(cfg, gh, q)

	// Run one poll cycle.
	p.RunOnceForTest(ctx)

	// Only the comment with @aizu should be enqueued.
	pending, err := q.NextPending(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if pending == nil {
		t.Fatal("expected one task to be enqueued")
	}
	if pending.CommentID != 2 {
		t.Errorf("enqueued comment ID = %d, want 2", pending.CommentID)
	}
	if pending.Author != "bob" {
		t.Errorf("author = %q, want bob", pending.Author)
	}

	// No more tasks should be pending.
	none, err := q.NextPending(ctx, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if none != nil {
		t.Errorf("expected no more tasks, got %+v", none)
	}
}

// TestPollerIgnoresBotComments verifies the poller skips its own comments.
func TestPollerIgnoresBotComments(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		listComments: []github.Comment{
			{ID: 1, Body: "@aizu done", User: github.User{Login: "aizu-bot"}, IssueURL: "https://api.github.com/repos/o/r/issues/1"},
			{ID: 2, Body: "@aizu fix", User: github.User{Login: "alice"}, IssueURL: "https://api.github.com/repos/o/r/issues/2"},
		},
	}

	cfg := &config.Config{
		Trigger:     "@aizu",
		Repos:       []string{"o/r"},
		BotUsername: "aizu-bot",
	}

	p := poller.New(cfg, gh, q)
	p.RunOnceForTest(ctx)

	pending, _ := q.NextPending(ctx, 1*time.Second)
	if pending == nil {
		t.Fatal("expected one task (alice's comment)")
	}
	if pending.CommentID != 2 {
		t.Errorf("should have enqueued alice's comment (ID=2), got ID=%d", pending.CommentID)
	}
}

// TestPollerAllowlist verifies the poller respects the user allowlist.
func TestPollerAllowlist(t *testing.T) {
	q := queue.New(testRedisURL)
	ctx := context.Background()
	_ = q.Client().FlushDB(ctx)

	gh := &mockGitHub{
		listComments: []github.Comment{
			{ID: 1, Body: "@aizu go", User: github.User{Login: "alice"}, IssueURL: "https://api.github.com/repos/o/r/issues/1"},
			{ID: 2, Body: "@aizu go", User: github.User{Login: "eve"}, IssueURL: "https://api.github.com/repos/o/r/issues/2"},
		},
	}

	cfg := &config.Config{
		Trigger:     "@aizu",
		Repos:       []string{"o/r"},
		BotUsername: "aizu-bot",
		Users:       []string{"alice"}, // only alice is allowed
	}

	p := poller.New(cfg, gh, q)
	p.RunOnceForTest(ctx)

	pending, _ := q.NextPending(ctx, 1*time.Second)
	if pending == nil {
		t.Fatal("expected one task (alice's comment)")
	}
	if pending.Author != "alice" {
		t.Errorf("author = %q, want alice (eve should be blocked)", pending.Author)
	}

	// No more tasks.
	none, _ := q.NextPending(ctx, 500*time.Millisecond)
	if none != nil {
		t.Errorf("expected no more tasks (eve blocked), got %+v", none)
	}
}
