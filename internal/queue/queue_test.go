// Integration tests for the Redis-backed queue.
//
// These tests require a running Redis instance. Run with:
//
//	docker run -d --name aizu-test-redis -p 6379:6379 redis:7-alpine
//	go test -race -tags=integration ./internal/queue
//	docker rm -f aizu-test-redis
//
// Or use the Makefile target: make test-integration
//
//go:build integration

package queue

import (
	"context"
	"testing"
	"time"
)

const testRedisURL = "redis://localhost:6379"

func testQueue(t *testing.T) *Queue {
	t.Helper()
	q := New(testRedisURL)
	ctx := context.Background()

	// Flush the database so each test starts clean.
	if err := q.rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush db: %v", err)
	}
	return q
}

func TestEnqueueAndNextPending(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "owner/repo", 42, 111, "@aizu fix this", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if task == nil {
		t.Fatal("Enqueue returned nil task")
	}
	if task.Repo != "owner/repo" || task.Number != 42 {
		t.Errorf("task = %+v, want repo=owner/repo number=42", task)
	}

	pending, err := q.NextPending(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if pending == nil {
		t.Fatal("NextPending returned nil")
	}
	if pending.ID != task.ID {
		t.Errorf("pending.ID = %s, want %s", pending.ID, task.ID)
	}
}

func TestEnqueueDeduplication(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	// First enqueue for issue #42 should succeed.
	task1, err := q.Enqueue(ctx, "owner/repo", 42, 111, "@aizu fix", "alice")
	if err != nil || task1 == nil {
		t.Fatalf("first Enqueue: task=%v, err=%v", task1, err)
	}

	// Second enqueue for the same issue #42 (different comment) should be skipped.
	task2, err := q.Enqueue(ctx, "owner/repo", 42, 222, "@aizu also fix", "bob")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if task2 != nil {
		t.Errorf("second Enqueue should return nil (deduplicated), got %+v", task2)
	}

	// Enqueue for a different issue (#99) should succeed.
	task3, err := q.Enqueue(ctx, "owner/repo", 99, 333, "@aizu help", "carol")
	if err != nil || task3 == nil {
		t.Fatalf("third Enqueue: task=%v, err=%v", task3, err)
	}
	if task3.ID == task1.ID {
		t.Error("different issues should get different task IDs")
	}
}

func TestEnqueueDeduplicationClearedAfterDone(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "o/r", 1, 10, "@aizu", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: task=%v, err=%v", task, err)
	}

	pending, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || pending == nil {
		t.Fatalf("NextPending: task=%v, err=%v", pending, err)
	}

	q.MarkDone(ctx, pending)

	// After MarkDone, a new enqueue for the same issue should succeed.
	task2, err := q.Enqueue(ctx, "o/r", 1, 20, "@aizu again", "bob")
	if err != nil {
		t.Fatalf("second Enqueue after done: %v", err)
	}
	if task2 == nil {
		t.Error("second Enqueue after MarkDone should succeed (not nil)")
	}
}

func TestMarkFailedRetries(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "o/r", 5, 1, "@aizu", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: %v", err)
	}

	pending, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || pending == nil {
		t.Fatalf("NextPending: %v", err)
	}

	// First failure should re-queue the task.
	requeued := q.MarkFailed(ctx, pending)
	if !requeued {
		t.Error("MarkFailed should re-queue on first failure")
	}

	// Task should be retrievable again.
	pending2, err := q.NextPending(ctx, 5*time.Second)
	if err != nil || pending2 == nil {
		t.Fatalf("NextPending after retry: task=%v, err=%v", pending2, err)
	}
	if pending2.Retries != 1 {
		t.Errorf("retried task Retries = %d, want 1", pending2.Retries)
	}
}

func TestMarkFailedDeadLettersAfterMaxRetries(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "o/r", 7, 1, "@aizu", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Process and fail (retries goes to 1, which equals maxRetries).
	pending, _ := q.NextPending(ctx, 2*time.Second)
	requeued := q.MarkFailed(ctx, pending)
	if !requeued {
		t.Error("first MarkFailed should re-queue")
	}

	// Process and fail again — should dead-letter (retries = 2 > maxRetries = 1).
	pending2, _ := q.NextPending(ctx, 5*time.Second)
	requeued2 := q.MarkFailed(ctx, pending2)
	if requeued2 {
		t.Error("MarkFailed should NOT re-queue after max retries exceeded")
	}

	// No more tasks should be pending.
	none, err := q.NextPending(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if none != nil {
		t.Errorf("expected no pending tasks after dead-letter, got %+v", none)
	}
}

func TestNextPendingTimeout(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	// Empty queue should return nil on timeout.
	start := time.Now()
	task, err := q.NextPending(ctx, 500*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("NextPending on empty queue: %v", err)
	}
	if task != nil {
		t.Error("NextPending on empty queue should return nil")
	}
	if elapsed < 400*time.Millisecond {
		t.Errorf("NextPending returned too quickly (%v), should have waited for timeout", elapsed)
	}
}

func TestDifferentReposNotDeduplicated(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	task1, err := q.Enqueue(ctx, "owner/repo-a", 1, 1, "@aizu", "alice")
	if err != nil || task1 == nil {
		t.Fatalf("Enqueue repo-a: %v", err)
	}

	task2, err := q.Enqueue(ctx, "owner/repo-b", 1, 2, "@aizu", "bob")
	if err != nil || task2 == nil {
		t.Fatalf("Enqueue repo-b: %v", err)
	}

	if task1.ID == task2.ID {
		t.Error("tasks for different repos should have different IDs")
	}

	// Both should be retrievable.
	p1, _ := q.NextPending(ctx, 2*time.Second)
	p2, _ := q.NextPending(ctx, 2*time.Second)
	if p1 == nil || p2 == nil {
		t.Error("both tasks should be retrievable")
	}
}
