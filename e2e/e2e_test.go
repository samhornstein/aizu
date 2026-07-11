//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/poller"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
	"github.com/samhornstein/aizu/internal/worker"
)

type mockExec struct{}

func (m *mockExec) Create(repo, branch string, prNumber int) (string, error) {
	return "test-sid", nil
}
func (m *mockExec) RunEngine(sid, prompt string) (int, string, error) {
	return 0, "agent output", nil
}
func (m *mockExec) ReadFile(sid, path string) (string, error) {
	return "", errors.New("no file")
}
func (m *mockExec) Destroy(sid string) {}
func (m *mockExec) CleanupStale()      {}

// TestPipeline exercises the full flow: poller detects a GitHub comment,
// enqueues to Redis, worker picks it up, runs the mock agent, and posts a reply.
func TestPipeline(t *testing.T) {
	var pollCount int32
	replyCh := make(chan string, 1)

	mux := http.NewServeMux()

	// Single-account mode: the token belongs to the same account that posts
	// the trigger comment. The marker-based self-filter makes this work.
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"login": "alice", "type": "User"})
	})

	mux.HandleFunc("/repos/o/r/issues/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&pollCount, 1) == 1 {
			json.NewEncoder(w).Encode([]map[string]any{{
				"id":        float64(123),
				"body":      "@aizu please help",
				"user":      map[string]string{"login": "alice"},
				"issue_url": "https://api.github.com/repos/o/r/issues/1",
			}})
		} else {
			json.NewEncoder(w).Encode([]any{})
		}
	})

	// The collaborator gate consults this before enqueueing; without it the
	// trigger would be denied (which is itself proof the gate is on).
	mux.HandleFunc("/repos/o/r/collaborators/alice/permission", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"permission": "write"}) //nolint:errcheck
	})

	mux.HandleFunc("/repos/o/r/issues/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 1,
			"title":  "Test issue",
			"body":   "Please help with something.",
		})
	})

	mux.HandleFunc("/repos/o/r/issues/comments/123/reactions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{})
	})

	mux.HandleFunc("/repos/o/r/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload) //nolint:errcheck
		select {
		case replyCh <- payload["body"]:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/1"
	}
	q := queue.New(redisURL)
	if err := q.Client().FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() { q.Client().FlushDB(context.Background()) }) //nolint:errcheck

	cfg := &config.Config{
		Trigger:      "@aizu",
		Repos:        []string{"o/r"},
		PollInterval: 200 * time.Millisecond,
		BotUsername:  "alice",
	}

	gh := github.NewWithBaseURL("test-token", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go poller.New(cfg, gh, q).Run(ctx)
	go worker.New(q, &mockExec{}, gh, template.NewLoader("be helpful"), cfg).Run(ctx)

	select {
	case reply := <-replyCh:
		if reply == "" {
			t.Fatal("got empty reply from worker")
		}
		want := "agent output\n\n" + github.ReplyMarker
		if reply != want {
			t.Errorf("reply = %q, want %q", reply, want)
		}
		t.Logf("pipeline produced reply: %s", reply)
	case <-ctx.Done():
		t.Fatal("timed out waiting for reply — pipeline did not complete")
	}
}
