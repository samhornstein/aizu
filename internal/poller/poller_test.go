package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
)

func newTestPoller(cfg *config.Config) *Poller {
	return &Poller{cfg: cfg}
}

func TestShouldTriggerKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when comment begins with the keyword")
	}
}

func TestShouldTriggerKeywordMidComment(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "hey @aizu fix this", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when the keyword is not at the start")
	}
}

func TestShouldTriggerMissingKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}

func TestShouldTriggerIgnoresBotComment(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", BotUsername: "aizu-bot"})

	c := github.Comment{Body: "@aizu done.", User: github.User{Login: "aizu-bot"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false for bot's own comment")
	}
}

func TestShouldTriggerAllowlist(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", Users: []string{"alice", "bob"}})

	allowed := github.Comment{Body: "@aizu go", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", allowed) {
		t.Error("shouldTrigger() = false, want true for allowlisted user")
	}

	blocked := github.Comment{Body: "@aizu go", User: github.User{Login: "eve"}}
	if p.shouldTrigger("owner/repo", blocked) {
		t.Error("shouldTrigger() = true, want false for non-allowlisted user")
	}
}

func TestShouldTriggerEmptyAllowlistPermitsAll(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", Users: nil})

	c := github.Comment{Body: "@aizu help", User: github.User{Login: "anyone"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when allowlist is empty (permit all)")
	}
}

func TestContains(t *testing.T) {
	list := []string{"alice", "bob"}
	if !contains(list, "alice") {
		t.Error("contains(alice) = false, want true")
	}
	if contains(list, "eve") {
		t.Error("contains(eve) = true, want false")
	}
	if contains(nil, "alice") {
		t.Error("contains(nil, alice) = true, want false")
	}
}

// newLivePoller wires a Poller to miniredis and a fake GitHub API server.
func newLivePoller(t *testing.T, cfg *config.Config, handler http.Handler) (*Poller, *queue.Queue) {
	t.Helper()
	mr := miniredis.RunT(t)
	q := queue.New("redis://" + mr.Addr())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(cfg, github.NewWithBaseURL("test-token", srv.URL), q), q
}

func queueLen(t *testing.T, q *queue.Queue) int64 {
	t.Helper()
	n, err := q.Client().LLen(context.Background(), "aizu:tasks").Result()
	if err != nil {
		t.Fatalf("LLEN: %v", err)
	}
	return n
}

// popAndComplete consumes one task from the queue and marks it done, as the
// worker would, so the issue/PR is no longer "active".
func popAndComplete(t *testing.T, q *queue.Queue) *queue.Task {
	t.Helper()
	ctx := context.Background()
	id, err := q.Client().RPop(ctx, "aizu:tasks").Result()
	if err != nil {
		t.Fatalf("RPOP: %v", err)
	}
	data, err := q.Client().Get(ctx, "aizu:task:"+id).Result()
	if err != nil {
		t.Fatalf("GET task: %v", err)
	}
	var task queue.Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	q.MarkDone(ctx, &task)
	return &task
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// TestIssueBodyTriggersExactlyOnce reproduces the re-trigger loop: the same
// issue keeps coming back from GitHub with a bumped updated_at (as happens
// when the bot's own reply updates the issue), and must not re-enqueue after
// its task completed.
func TestIssueBodyTriggersExactlyOnce(t *testing.T) {
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		// Same issue every poll, updated_at bumped each time.
		updated := "2026-07-10T10:00:00Z"
		if atomic.AddInt32(&polls, 1) > 1 {
			updated = "2026-07-10T11:00:00Z"
		}
		writeJSON(t, w, []map[string]any{{
			"number":     7,
			"title":      "Build the thing",
			"body":       "@aizu build this",
			"user":       map[string]string{"login": "alice"},
			"updated_at": updated,
		}})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)
	ctx := context.Background()

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 1 {
		t.Fatalf("after first poll: queue len = %d, want 1", n)
	}
	popAndComplete(t, q)

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 0 {
		t.Errorf("after second poll: queue len = %d, want 0 (issue must not re-trigger)", n)
	}
}

// TestEditedCommentDoesNotRetrigger: a triggering comment that reappears with
// a later updated_at (i.e. it was edited) must not run the agent again.
func TestEditedCommentDoesNotRetrigger(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{{
			"id":        int64(42),
			"body":      "@aizu fix this",
			"user":      map[string]string{"login": "alice"},
			"issue_url": "https://api.github.com/repos/o/r/issues/1",
		}})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)
	ctx := context.Background()

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 1 {
		t.Fatalf("after first poll: queue len = %d, want 1", n)
	}
	popAndComplete(t, q)

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 0 {
		t.Errorf("after second poll: queue len = %d, want 0 (edited comment must not re-trigger)", n)
	}
}

// TestDistinctCommentsBothTrigger: the seen-marker is per comment ID, so a new
// comment must still trigger.
func TestDistinctCommentsBothTrigger(t *testing.T) {
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		id, issue := int64(42), "1"
		if atomic.AddInt32(&polls, 1) > 1 {
			id, issue = int64(43), "2"
		}
		writeJSON(t, w, []map[string]any{{
			"id":        id,
			"body":      "@aizu go",
			"user":      map[string]string{"login": "alice"},
			"issue_url": "https://api.github.com/repos/o/r/issues/" + issue,
		}})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)
	ctx := context.Background()

	p.pollOnce(ctx)
	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 2 {
		t.Errorf("queue len = %d, want 2 (distinct comments must both trigger)", n)
	}
}

// TestActiveSkipKeepsMarker: when Enqueue skips because the issue already has
// an active task (returns nil, nil — not an error), the seen-marker must be
// kept so the skipped trigger doesn't fire later.
func TestActiveSkipKeepsMarker(t *testing.T) {
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		// Poll 1: comment 42. Polls 2+: comment 43, same issue.
		id := int64(42)
		if atomic.AddInt32(&polls, 1) > 1 {
			id = 43
		}
		writeJSON(t, w, []map[string]any{{
			"id":        id,
			"body":      "@aizu go",
			"user":      map[string]string{"login": "alice"},
			"issue_url": "https://api.github.com/repos/o/r/issues/1",
		}})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)
	ctx := context.Background()

	p.pollOnce(ctx) // comment 42 enqueued; issue 1 now active
	p.pollOnce(ctx) // comment 43: marker set, Enqueue skips (issue active)
	if n := queueLen(t, q); n != 1 {
		t.Fatalf("queue len = %d, want 1 (second comment skipped while issue active)", n)
	}

	popAndComplete(t, q)

	p.pollOnce(ctx) // comment 43 again: marker must suppress it
	if n := queueLen(t, q); n != 0 {
		t.Errorf("after completion: queue len = %d, want 0 (skipped trigger must not fire later)", n)
	}
}

func TestShouldTriggerIssueBody(t *testing.T) {
	// The shouldTrigger function works on comments. For issue body triggers,
	// the poller uses inline checks in pollIssues instead.
	// Verify that the keyword check logic is consistent.
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	// A comment with the keyword should trigger.
	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true for matching keyword")
	}

	// A comment without the keyword should not trigger.
	c2 := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c2) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}
