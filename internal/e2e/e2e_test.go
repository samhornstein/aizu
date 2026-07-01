// Package e2e contains end-to-end integration tests that exercise the full
// Aizu pipeline: poller → queue → worker → executor.
//
// These tests require Redis and Docker to be available. They are skipped
// automatically if either dependency is missing.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/executor"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
)

// testConfig returns a minimal config for e2e tests.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	containerImage := os.Getenv("TEST_CONTAINER_IMAGE")
	if containerImage == "" {
		containerImage = "alpine:3.20"
	}
	return &config.Config{
		RedisURL:       redisURL,
		Trigger:        "@aizu",
		Repos:          []string{"owner/repo"},
		ContainerImage: containerImage,
		EngineCommand:  "echo 'agent response'",
		Timeout:        30,
		PollInterval:   1 * time.Second,
		GitHubToken:    "test-token",
		BotUsername:    "aizu-bot",
	}
}

// skipIfNoDocker skips the test if Docker is not available.
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found, skipping e2e test")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable, skipping e2e test")
	}
}

// skipIfNoRedis skips the test if Redis is not available.
func skipIfNoRedis(t *testing.T) {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	// Quick connectivity check.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo ping | nc -w 1 %s 6379", strings.Split(redisURL, "//")[1]))
	if err := cmd.Run(); err != nil {
		t.Skip("redis not reachable, skipping e2e test")
	}
}

// TestE2EFullPipeline tests the complete pipeline: enqueue → dequeue →
// sandbox creation → engine execution → task completion.
func TestE2EFullPipeline(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoRedis(t)

	cfg := testConfig(t)

	ctx := context.Background()

	// Clean up Redis keys from previous runs.
	q := queue.New(cfg.RedisURL)
	rdb := q.Client()
	keys, _ := rdb.Keys(ctx, "aizu:*").Result()
	if len(keys) > 0 {
		rdb.Del(ctx, keys...).Err()
	}

	// Step 1: Enqueue a task (simulates the poller finding a triggered comment).
	task, err := q.Enqueue(ctx, "owner/repo", 42, 100, "@aizu fix this", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if task == nil {
		t.Fatal("expected task, got nil")
	}

	// Step 2: Dequeue the task (simulates the worker picking it up).
	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Body != "@aizu fix this" {
		t.Errorf("task body = %q, want '@aizu fix this'", got.Body)
	}

	// Step 3: Create a sandbox (simulates the worker creating a container).
	exec := executor.New(cfg)
	exec.CleanupStale()
	defer exec.CleanupStale()

	sid, err := exec.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	defer exec.Destroy(sid)

	// Step 4: Build a prompt (simulates the worker building context).
	loader := template.NewLoader("You are a helpful coding agent.")
	prompt := loader.Resolve(exec, sid)
	prompt += "\n\n---\n\nResponding to comment on issue #42.\n"
	prompt += fmt.Sprintf("The comment from @%s: %s\n", got.Author, got.Body)

	// Step 5: Run the engine in the sandbox.
	exitCode, output, err := exec.RunEngine(sid, prompt)
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, output: %s", exitCode, output)
	}
	if !strings.Contains(strings.TrimSpace(output), "agent response") {
		t.Errorf("output = %q, want to contain 'agent response'", output)
	}

	// Step 6: Mark the task as done (simulates the worker completing the task).
	q.MarkDone(ctx, got)

	// Verify the task blob is cleaned up.
	exists := rdb.Exists(ctx, "aizu:task:"+got.ID).Val()
	if exists != 0 {
		t.Error("task should be deleted after MarkDone")
	}
}

// TestE2EQueueDeduplication verifies that the poller→queue path correctly
// deduplicates comments on the same issue/PR.
func TestE2EQueueDeduplication(t *testing.T) {
	skipIfNoRedis(t)

	cfg := testConfig(t)
	q := queue.New(cfg.RedisURL)
	ctx := context.Background()

	// Clean up.
	rdb := q.Client()
	keys, _ := rdb.Keys(ctx, "aizu:*").Result()
	if len(keys) > 0 {
		rdb.Del(ctx, keys...).Err()
	}

	// First comment triggers a task.
	t1, err := q.Enqueue(ctx, "owner/repo", 1, 100, "@aizu first", "alice")
	if err != nil || t1 == nil {
		t.Fatalf("first Enqueue: task=%v, err=%v", t1, err)
	}

	// Second comment on same issue while task is pending should be deduplicated.
	t2, err := q.Enqueue(ctx, "owner/repo", 1, 101, "@aizu second", "bob")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if t2 != nil {
		t.Errorf("expected nil for duplicate, got %+v", t2)
	}

	// Dequeue and complete.
	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got == nil {
		t.Fatalf("NextPending: task=%v, err=%v", got, err)
	}
	q.MarkDone(ctx, got)

	// After MarkDone, a new comment should succeed.
	t3, err := q.Enqueue(ctx, "owner/repo", 1, 102, "@aizu third", "carol")
	if err != nil || t3 == nil {
		t.Fatalf("third Enqueue after MarkDone: task=%v, err=%v", t3, err)
	}
}

// TestE2EExecutorTimeout verifies the executor correctly times out long-running engines.
func TestE2EExecutorTimeout(t *testing.T) {
	skipIfNoDocker(t)

	cfg := testConfig(t)
	cfg.EngineCommand = "sleep 30"
	cfg.Timeout = 3 // 3 second timeout

	exec := executor.New(cfg)
	defer exec.CleanupStale()

	sid, err := exec.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer exec.Destroy(sid)

	exitCode, _, err := exec.RunEngine(sid, "test")
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if exitCode != 124 {
		t.Errorf("exit code = %d, want 124 (timeout)", exitCode)
	}
}
