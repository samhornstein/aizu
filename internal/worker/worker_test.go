package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
)

// mockExecutor satisfies executor.Executor with a controllable ReadFile response.
type mockExecutor struct {
	fileContent string
	fileErr     error
	engineExit  int
	engineOut   string

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
	return m.engineExit, m.engineOut, nil
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
				gh:     github.NewWithBaseURL("t", srv.URL, false),
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

func TestRedact(t *testing.T) {
	got := redact("token ghp_abc and key sk-x", "ghp_abc", "", "sk-x")
	if got != "token [redacted] and key [redacted]" {
		t.Errorf("redact() = %q", got)
	}
	if redact("clean text") != "clean text" {
		t.Error("redact with no secrets must be a no-op")
	}
}

// TestProcessRedactsSecrets runs process() end to end (miniredis + fake
// GitHub) and asserts the posted comment never contains a configured secret —
// on the success path and on the engine-failure path.
func TestProcessRedactsSecrets(t *testing.T) {
	const token = "ghp_supersecret"
	cases := []struct {
		name     string
		exitCode int
	}{
		{"success path", 0},
		{"engine failure path", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var posted string
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/o/r/issues/9", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"number": 9, "title": "T"})
			})
			mux.HandleFunc("/repos/o/r/issues/9/comments", func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]string
				_ = json.NewDecoder(r.Body).Decode(&payload)
				posted = payload["body"]
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("{}"))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			mr := miniredis.RunT(t)
			w := New(
				queue.New("redis://"+mr.Addr()),
				&mockExecutor{fileErr: errors.New("no file"), engineExit: tc.exitCode, engineOut: "leaked: " + token},
				github.NewWithBaseURL("t", srv.URL, false),
				template.NewLoader("sys"),
				&config.Config{GitHubToken: token},
			)
			// Retries exhausted so the failure path posts instead of re-queueing.
			task := &queue.Task{ID: "t1", Repo: "o/r", Number: 9, CommentID: 0, Author: "alice", Body: "@aizu", Retries: 1}

			w.process(context.Background(), task)

			if posted == "" {
				t.Fatal("no comment was posted")
			}
			if strings.Contains(posted, token) {
				t.Fatalf("posted comment contains the token: %s", posted)
			}
			if !strings.Contains(posted, "[redacted]") {
				t.Errorf("posted comment should carry the redaction marker; got: %s", posted)
			}
		})
	}
}

// feedbackServer is a fake GitHub server recording reactions, comment posts,
// and comment edits for process()-level tests.
type feedbackServer struct {
	mu            sync.Mutex
	reactions     []string
	posts         []string
	patches       map[int64]string
	failFirstPost bool   // 500 the first comment POST (the placeholder)
	issueState    string // issue state served by GET; empty = open
	postsSeen     int
	nextID        int64
}

func newFeedbackServer(t *testing.T) (*feedbackServer, *httptest.Server) {
	t.Helper()
	fs := &feedbackServer{patches: map[int64]string{}, nextID: 100}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/9", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 9, "title": "T", "state": fs.issueState})
	})
	mux.HandleFunc("/repos/o/r/issues/comments/777/reactions", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		fs.mu.Lock()
		fs.reactions = append(fs.reactions, payload["content"])
		fs.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/repos/o/r/issues/9/comments", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.postsSeen++
		firstAndFailing := fs.failFirstPost && fs.postsSeen == 1
		fs.mu.Unlock()
		if firstAndFailing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		fs.mu.Lock()
		fs.posts = append(fs.posts, payload["body"])
		fs.nextID++
		id := fs.nextID
		fs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	})
	mux.HandleFunc("/repos/o/r/issues/comments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("comment edit method = %q, want PATCH", r.Method)
		}
		var id int64
		_, _ = fmt.Sscanf(r.URL.Path, "/repos/o/r/issues/comments/%d", &id)
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		fs.mu.Lock()
		fs.patches[id] = payload["body"]
		fs.mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fs, srv
}

func newFeedbackWorker(t *testing.T, srv *httptest.Server, exec *mockExecutor, cfg *config.Config) *Worker {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(queue.New("redis://"+mr.Addr()), exec, github.NewWithBaseURL("t", srv.URL, false), template.NewLoader("sys"), cfg)
}

func commentTask(id string) *queue.Task {
	return &queue.Task{ID: id, Repo: "o/r", Number: 9, CommentID: 777, Author: "alice", Body: "@aizu do it", Retries: 1}
}

func TestProcessPlaceholderAndReactions(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	exec := &mockExecutor{fileErr: errors.New("no file"), engineOut: "all done"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

	w.process(context.Background(), commentTask("t1"))

	if want := []string{"eyes", "rocket"}; !slices.Equal(fs.reactions, want) {
		t.Errorf("reactions = %v, want %v", fs.reactions, want)
	}
	if len(fs.posts) != 1 || !strings.Contains(fs.posts[0], "working") {
		t.Fatalf("posts = %v, want one placeholder containing 'working'", fs.posts)
	}
	if got := fs.patches[101]; !strings.Contains(got, "all done") {
		t.Errorf("placeholder was not edited into the result; patches = %v", fs.patches)
	}
}

func TestProcessFailureReactionAndEdit(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	exec := &mockExecutor{fileErr: errors.New("no file"), engineExit: 1, engineOut: "boom"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

	w.process(context.Background(), commentTask("t1"))

	if want := []string{"eyes", "confused"}; !slices.Equal(fs.reactions, want) {
		t.Errorf("reactions = %v, want %v", fs.reactions, want)
	}
	if got := fs.patches[101]; !strings.Contains(got, "failed") {
		t.Errorf("placeholder should carry the failure text; patches = %v", fs.patches)
	}
}

func TestProcessPlaceholderPostFailureStillReplies(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	exec := &mockExecutor{fileErr: errors.New("no file"), engineOut: "result"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

	fs.failFirstPost = true
	w.process(context.Background(), commentTask("t1"))

	if len(fs.posts) != 1 || !strings.Contains(fs.posts[0], "result") {
		t.Errorf("result must arrive as a new comment when the placeholder failed; posts = %v", fs.posts)
	}
	if len(fs.patches) != 0 {
		t.Errorf("nothing should be patched without a placeholder; patches = %v", fs.patches)
	}
}

func TestHelpRequest(t *testing.T) {
	for _, body := range []string{"@aizu help", "@aizu", "  @aizu   HELP  "} {
		t.Run(body, func(t *testing.T) {
			fs, srv := newFeedbackServer(t)
			exec := &mockExecutor{fileErr: errors.New("no file")}
			w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

			task := commentTask("t1")
			task.Body = body
			w.process(context.Background(), task)

			if len(fs.posts) != 1 || !strings.Contains(fs.posts[0], "implement") {
				t.Fatalf("posts = %v, want one help reply", fs.posts)
			}
			if exec.gotPrompt != "" {
				t.Error("help must not run the engine")
			}
		})
	}
}

func TestHelpNotTriggeredByPrefix(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	exec := &mockExecutor{fileErr: errors.New("no file"), engineOut: "done"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

	task := commentTask("t1")
	task.Body = "@aizu helpme do X"
	w.process(context.Background(), task)

	if exec.gotPrompt == "" {
		t.Error("'helpme' is a real request and must reach the engine")
	}
	if len(fs.posts) == 0 || strings.Contains(fs.posts[0], "implement") {
		t.Errorf("must not post the help text; posts = %v", fs.posts)
	}
}

func TestRateLimit(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	exec := &mockExecutor{fileErr: errors.New("no file"), engineOut: "ok"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu", MaxRunsPerHour: 2})
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		w.process(ctx, commentTask(fmt.Sprintf("t%d", i)))
	}
	if exec.gotPrompt == "" {
		t.Fatal("runs within the limit must reach the engine")
	}
	postsBefore := len(fs.posts)

	// Third task trips the limit: one limit reply, no engine run.
	exec.gotPrompt = ""
	w.process(ctx, commentTask("t3"))
	if exec.gotPrompt != "" {
		t.Error("rate-limited task must not run the engine")
	}
	if len(fs.posts) != postsBefore+1 || !strings.Contains(fs.posts[len(fs.posts)-1], "rate limit") {
		t.Fatalf("want exactly one limit reply; posts = %v", fs.posts)
	}

	// Fourth is dropped silently.
	w.process(ctx, commentTask("t4"))
	if len(fs.posts) != postsBefore+1 {
		t.Errorf("fourth task must not post; posts = %v", fs.posts)
	}
}

// TestClosedIssueSkipped: a trigger on a closed issue must not run the
// engine or count as a failure; the placeholder is edited into a skip note.
func TestClosedIssueSkipped(t *testing.T) {
	fs, srv := newFeedbackServer(t)
	fs.issueState = "closed"
	exec := &mockExecutor{fileErr: errors.New("no file"), engineOut: "should not run"}
	w := newFeedbackWorker(t, srv, exec, &config.Config{Trigger: "@aizu"})

	w.process(context.Background(), commentTask("t1"))

	if exec.gotPrompt != "" {
		t.Error("closed issue must not run the engine")
	}
	if got := fs.patches[101]; !strings.Contains(got, "closed") {
		t.Errorf("placeholder should be edited into the skip note; patches = %v", fs.patches)
	}
	if slices.Contains(fs.reactions, "confused") {
		t.Error("a closed-issue skip is not a failure; no 😕 reaction")
	}
}

// blockingExecutor parks every RunEngine call until release is closed, so a
// test can observe how many runs are in flight at once.
type blockingExecutor struct {
	entered chan int
	release chan struct{}
}

func (b *blockingExecutor) Create(repo, branch string, prNumber int) (string, error) {
	return "sid", nil
}
func (b *blockingExecutor) RunEngine(sid, prompt string) (int, string, error) {
	b.entered <- 1
	<-b.release
	return 0, "done", nil
}
func (b *blockingExecutor) ReadFile(sid, path string) (string, error) {
	return "", errors.New("no file")
}
func (b *blockingExecutor) Destroy(sid string) {}
func (b *blockingExecutor) CleanupStale()      {}

// TestConcurrentWorkersRunInParallel: two Run goroutines sharing one Worker
// process tasks for two different issues simultaneously, while the queue's
// dedupe keeps a second task for an already-active issue out entirely.
func TestConcurrentWorkersRunInParallel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "title": "T"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mr := miniredis.RunT(t)
	q := queue.New("redis://" + mr.Addr())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, n := range []int{1, 2} {
		if task, err := q.Enqueue(ctx, "o/r", n, 0, "@aizu go", "alice"); err != nil || task == nil {
			t.Fatalf("enqueue issue %d: task=%v err=%v", n, task, err)
		}
	}
	// Same-issue dedupe while queued.
	if task, _ := q.Enqueue(ctx, "o/r", 1, 0, "@aizu again", "alice"); task != nil {
		t.Fatal("second task for a queued issue must be rejected")
	}

	be := &blockingExecutor{entered: make(chan int), release: make(chan struct{})}
	w := New(q, be, github.NewWithBaseURL("t", srv.URL, false), template.NewLoader("sys"), &config.Config{Trigger: "@aizu"})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Run(ctx)
		}()
	}

	// Both tasks must be inside RunEngine at the same time.
	for i := 0; i < 2; i++ {
		select {
		case <-be.entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d task(s) entered RunEngine; want 2 concurrently", i)
		}
	}

	// Same-issue dedupe while running.
	if task, _ := q.Enqueue(ctx, "o/r", 1, 0, "@aizu again", "alice"); task != nil {
		t.Error("second task for a running issue must be rejected")
	}

	close(be.release)
	cancel()
	wg.Wait()
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
