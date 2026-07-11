package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
)

func newTestPoller(cfg *config.Config) *Poller {
	return &Poller{cfg: cfg}
}

func TestShouldTriggerKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", AllowAll: true})

	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when comment begins with the keyword")
	}
}

func TestShouldTriggerKeywordMidComment(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "hey @aizu fix this", User: github.User{Login: "alice"}}
	if p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when the keyword is not at the start")
	}
}

func TestShouldTriggerMissingKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}

func TestShouldTriggerIgnoresMarkedReply(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{
		Body: "@aizu done.\n\n" + github.ReplyMarker,
		User: github.User{Login: "aizu-bot"},
	}
	if p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = true, want false for a marker-stamped reply")
	}
}

func TestShouldTriggerSameAccountComment(t *testing.T) {
	// Single-account mode: the token's login equals the triggering user's.
	// An unmarked trigger comment from that account must still fire.
	p := newTestPoller(&config.Config{Trigger: "@aizu", BotUsername: "alice", AllowAll: true})

	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when the user triggers with their own token's account")
	}
}

func TestShouldTriggerAllowlist(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", Users: []string{"alice", "bob"}})

	allowed := github.Comment{Body: "@aizu go", User: github.User{Login: "alice"}}
	if !p.shouldTrigger(context.Background(), "owner/repo", allowed) {
		t.Error("shouldTrigger() = false, want true for allowlisted user")
	}

	blocked := github.Comment{Body: "@aizu go", User: github.User{Login: "eve"}}
	if p.shouldTrigger(context.Background(), "owner/repo", blocked) {
		t.Error("shouldTrigger() = true, want false for non-allowlisted user")
	}
}

func TestShouldTriggerAllowAll(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", AllowAll: true})

	c := github.Comment{Body: "@aizu help", User: github.User{Login: "anyone"}}
	if !p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = false, want true with AIZU_ALLOW_ALL")
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
	payload, err := q.Client().RPop(ctx, "aizu:tasks").Result()
	if err != nil {
		t.Fatalf("RPOP: %v", err)
	}
	id, _, _ := strings.Cut(payload, "|") // list entries are "<taskID>|<repo#number>"
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

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
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

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
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

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
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

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
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

// TestSingleAccountModeEndToEnd is the headline case for single-account mode:
// the poller and worker share one identity. Poll 1 returns a trigger comment
// authored by the token's own account — it must enqueue. Poll 2 returns Aizu's
// marker-stamped reply from the same account — it must not.
func TestSingleAccountModeEndToEnd(t *testing.T) {
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		comment := map[string]any{
			"id":        int64(42),
			"body":      "@aizu fix this",
			"user":      map[string]string{"login": "alice"},
			"issue_url": "https://api.github.com/repos/o/r/issues/1",
		}
		if atomic.AddInt32(&polls, 1) > 1 {
			comment["id"] = int64(43)
			comment["body"] = "Done, opened a PR.\n\n" + github.ReplyMarker
		}
		writeJSON(t, w, []map[string]any{comment})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})

	// BotUsername equals the commenter's login, as it does with a personal PAT.
	cfg := &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, BotUsername: "alice", AllowAll: true}
	p, q := newLivePoller(t, cfg, mux)
	ctx := context.Background()

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 1 {
		t.Fatalf("after first poll: queue len = %d, want 1 (own-account trigger must run)", n)
	}
	popAndComplete(t, q)

	p.pollOnce(ctx)
	if n := queueLen(t, q); n != 0 {
		t.Errorf("after second poll: queue len = %d, want 0 (marker-stamped reply must not trigger)", n)
	}
}

// TestIssueBodyWithMarkerDoesNotTrigger: an issue whose body carries the
// reply marker (written by Aizu itself) must be skipped by pollIssues.
func TestIssueBodyWithMarkerDoesNotTrigger(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{{
			"number":     9,
			"title":      "Follow-up",
			"body":       "@aizu created this\n\n" + github.ReplyMarker,
			"user":       map[string]string{"login": "alice"},
			"updated_at": "2026-07-10T10:00:00Z",
		}})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
	p.pollOnce(context.Background())
	if n := queueLen(t, q); n != 0 {
		t.Errorf("queue len = %d, want 0 (marked issue body must not trigger)", n)
	}
}

// permGateMux returns a fake GitHub server serving one trigger comment from
// author, with a /permission endpoint that answers perm (or status, if not
// 200) and counts its hits.
func permGateMux(t *testing.T, author, perm string, status int, hits *int32) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{{
			"id":        int64(42),
			"body":      "@aizu go",
			"user":      map[string]string{"login": author},
			"issue_url": "https://api.github.com/repos/o/r/issues/1",
		}})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})
	mux.HandleFunc("/repos/o/r/collaborators/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeJSON(t, w, map[string]string{"permission": perm})
	})
	return mux
}

// TestPermissionGate: without an allowlist, only write/admin authors may
// trigger; API errors deny (fail closed).
func TestPermissionGate(t *testing.T) {
	cases := []struct {
		name   string
		perm   string
		status int
		want   int64
	}{
		{"write triggers", "write", http.StatusOK, 1},
		{"admin triggers", "admin", http.StatusOK, 1},
		{"read denied", "read", http.StatusOK, 0},
		{"none denied", "none", http.StatusOK, 0},
		{"404 fails closed", "", http.StatusNotFound, 0},
		{"500 fails closed", "", http.StatusInternalServerError, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			mux := permGateMux(t, "alice", tc.perm, tc.status, &hits)
			p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)

			p.pollOnce(context.Background())
			if n := queueLen(t, q); n != tc.want {
				t.Errorf("queue len = %d, want %d", n, tc.want)
			}
			if hits == 0 {
				t.Error("permission endpoint was never consulted")
			}
		})
	}
}

// TestAllowlistSkipsPermissionAPI: an explicit AIZU_USERS list decides alone —
// members trigger with no permission call, non-members are denied even with
// write access.
func TestAllowlistSkipsPermissionAPI(t *testing.T) {
	var hits int32
	mux := permGateMux(t, "alice", "write", http.StatusOK, &hits)
	cfg := &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, Users: []string{"alice"}}
	p, q := newLivePoller(t, cfg, mux)

	p.pollOnce(context.Background())
	if n := queueLen(t, q); n != 1 {
		t.Fatalf("queue len = %d, want 1 (allowlisted author)", n)
	}
	if hits != 0 {
		t.Errorf("permission endpoint hit %d times, want 0 with an allowlist", hits)
	}

	var hits2 int32
	mux2 := permGateMux(t, "bob", "write", http.StatusOK, &hits2)
	p2, q2 := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, Users: []string{"alice"}}, mux2)
	p2.pollOnce(context.Background())
	if n := queueLen(t, q2); n != 0 {
		t.Errorf("queue len = %d, want 0 (bob not allowlisted, even with write)", n)
	}
}

// TestAllowAllPermitsReadOnly: the escape hatch admits authors without write.
func TestAllowAllPermitsReadOnly(t *testing.T) {
	var hits int32
	mux := permGateMux(t, "alice", "read", http.StatusOK, &hits)
	cfg := &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}
	p, q := newLivePoller(t, cfg, mux)

	p.pollOnce(context.Background())
	if n := queueLen(t, q); n != 1 {
		t.Errorf("queue len = %d, want 1 with AIZU_ALLOW_ALL", n)
	}
	if hits != 0 {
		t.Errorf("permission endpoint hit %d times, want 0 with AIZU_ALLOW_ALL", hits)
	}
}

// TestPermissionCached: repeat comments by one author cost one API call.
func TestPermissionCached(t *testing.T) {
	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{
			{
				"id":        int64(42),
				"body":      "@aizu one",
				"user":      map[string]string{"login": "alice"},
				"issue_url": "https://api.github.com/repos/o/r/issues/1",
			},
			{
				"id":        int64(43),
				"body":      "@aizu two",
				"user":      map[string]string{"login": "alice"},
				"issue_url": "https://api.github.com/repos/o/r/issues/2",
			},
		})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})
	mux.HandleFunc("/repos/o/r/collaborators/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		writeJSON(t, w, map[string]string{"permission": "write"})
	})

	p, q := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}}, mux)
	p.pollOnce(context.Background())

	if n := queueLen(t, q); n != 2 {
		t.Fatalf("queue len = %d, want 2", n)
	}
	if hits != 1 {
		t.Errorf("permission endpoint hit %d times, want 1 (cached)", hits)
	}
}

// TestPollErrorThrottled: a repo that fails every poll is logged once per
// errLogInterval, not per tick; recovery clears the throttle state.
func TestPollErrorThrottled(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, []any{})
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []any{})
	})

	p, _ := newLivePoller(t, &config.Config{Trigger: "@aizu", Repos: []string{"o/r"}, AllowAll: true}, mux)
	ctx := context.Background()

	p.pollOnce(ctx)
	first, ok := p.lastErrLog["o/r"]
	if !ok {
		t.Fatal("first failing poll did not record a logged error")
	}

	p.pollOnce(ctx)
	if got := p.lastErrLog["o/r"]; !got.Equal(first) {
		t.Error("second failing poll inside the throttle window logged again (timestamp advanced)")
	}

	// Simulate the throttle window elapsing: the next failure logs again.
	p.lastErrLog["o/r"] = time.Now().Add(-errLogInterval - time.Second)
	p.pollOnce(ctx)
	if got := p.lastErrLog["o/r"]; !got.After(first) {
		t.Error("failure after the throttle window did not log again")
	}

	// Recovery clears the state so a future failure logs immediately.
	failing.Store(false)
	p.pollOnce(ctx)
	if _, ok := p.lastErrLog["o/r"]; ok {
		t.Error("successful poll did not clear the throttle state")
	}
}

func TestShouldTriggerIssueBody(t *testing.T) {
	// The shouldTrigger function works on comments. For issue body triggers,
	// the poller uses inline checks in pollIssues instead.
	// Verify that the keyword check logic is consistent.
	p := newTestPoller(&config.Config{Trigger: "@aizu", AllowAll: true})

	// A comment with the keyword should trigger.
	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger(context.Background(), "owner/repo", c) {
		t.Error("shouldTrigger() = false, want true for matching keyword")
	}

	// A comment without the keyword should not trigger.
	c2 := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger(context.Background(), "owner/repo", c2) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}
