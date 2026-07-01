package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisURL returns the Redis URL from the environment or falls back to localhost.
func redisURL() string {
	if v := os.Getenv("REDIS_URL"); v != "" {
		return v
	}
	return "redis://localhost:6379"
}

// skipIfNoRedis skips the test if Redis is unreachable.
func skipIfNoRedis(t *testing.T) {
	t.Helper()
	rdb := redis.NewClient(redis.Options{Addr: "localhost:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping integration test")
	}
}

func setupQueue(t *testing.T) *Queue {
	t.Helper()
	q := New(redisURL())
	ctx := context.Background()
	// Clean up all aizu keys so tests are isolated.
	keys, _ := q.rdb.Keys(ctx, "aizu:*").Result()
	if len(keys) > 0 {
		q.rdb.Del(ctx, keys...).Err()
	}
	return q
}

func TestQueueEnqueueAndNext(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "owner/repo", 1, 100, "@aizu fix this", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if task == nil {
		t.Fatal("Enqueue returned nil task")
	}
	if task.Repo != "owner/repo" || task.Number != 1 {
		t.Errorf("task = %+v, want repo=owner/repo number=1", task)
	}

	// Dequeue the task.
	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if got == nil {
		t.Fatal("NextPending returned nil")
	}
	if got.ID != task.ID {
		t.Errorf("dequeued task ID = %s, want %s", got.ID, task.ID)
	}

	// Queue should now be empty (BRPop times out).
	got2, err := q.NextPending(ctx, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NextPending on empty queue: %v", err)
	}
	if got2 != nil {
		t.Errorf("expected nil on empty queue, got %+v", got2)
	}
}

func TestQueueEnqueueDeduplication(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	_, err := q.Enqueue(ctx, "owner/repo", 1, 100, "@aizu first", "alice")
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}

	// Second enqueue for the same issue/PR should be deduplicated.
	task2, err := q.Enqueue(ctx, "owner/repo", 1, 101, "@aizu second", "bob")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if task2 != nil {
		t.Errorf("expected nil for duplicate enqueue, got %+v", task2)
	}

	// Dequeue the first task to clear the running state.
	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	q.MarkDone(ctx, got)

	// Now a new task for the same issue should succeed.
	task3, err := q.Enqueue(ctx, "owner/repo", 1, 102, "@aizu third", "carol")
	if err != nil {
		t.Fatalf("third Enqueue: %v", err)
	}
	if task3 == nil {
		t.Fatal("expected task after MarkDone, got nil")
	}
}

func TestQueueMarkFailedRetry(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "owner/repo", 5, 200, "@aizu", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got == nil {
		t.Fatalf("NextPending: task=%v, err=%v", got, err)
	}

	// Mark failed — should retry (maxRetries=1).
	requeued := q.MarkFailed(ctx, got)
	if !requeued {
		t.Fatal("expected task to be requeued on first failure")
	}

	// Dequeue the retried task.
	got2, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got2 == nil {
		t.Fatalf("NextPending after retry: task=%v, err=%v", got2, err)
	}
	if got2.Retries != 1 {
		t.Errorf("retried task retries = %d, want 1", got2.Retries)
	}

	// Mark failed again — should dead-letter (exceeds maxRetries).
	requeued2 := q.MarkFailed(ctx, got2)
	if requeued2 {
		t.Error("expected task to be dead-lettered on second failure")
	}
}

func TestQueueDeadLetter(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "owner/repo", 99, 300, "@aizu", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got == nil {
		t.Fatalf("NextPending: task=%v, err=%v", got, err)
	}

	// Mark failed twice to trigger dead-letter.
	q.MarkFailed(ctx, got)
	got2, _ := q.NextPending(ctx, 2*time.Second)
	if got2 != nil {
		q.MarkFailed(ctx, got2)
	}

	// Check dead-letter list has an entry.
	count := q.rdb.LLen(ctx, failedKey).Val()
	if count == 0 {
		t.Error("expected at least one dead-lettered task")
	}

	// Verify dead-lettered task data.
	data, _ := q.rdb.LPop(ctx, failedKey).Result()
	var dead Task
	json.Unmarshal([]byte(data), &dead)
	if dead.Repo != "owner/repo" || dead.Number != 99 {
		t.Errorf("dead-lettered task = %+v, want repo=owner/repo number=99", dead)
	}
}

func TestQueueMarkDoneRemovesTask(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "owner/repo", 42, 400, "@aizu", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got == nil {
		t.Fatalf("NextPending: task=%v, err=%v", got, err)
	}

	q.MarkDone(ctx, got)

	// Task blob should be deleted.
	exists := q.rdb.Exists(ctx, taskPrefix+got.ID).Val()
	if exists != 0 {
		t.Error("task blob should be deleted after MarkDone")
	}

	// Running set should not contain the active key.
	member := q.rdb.SIsMember(ctx, runningKey, activeKey("owner/repo", 42)).Val()
	if member {
		t.Error("running set should not contain completed task")
	}
}

func TestQueueMultipleRepos(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	// Enqueue tasks for different repos.
	t1, err := q.Enqueue(ctx, "owner/repo-a", 1, 1, "@aizu", "alice")
	if err != nil {
		t.Fatalf("Enqueue repo-a: %v", err)
	}
	t2, err := q.Enqueue(ctx, "owner/repo-b", 1, 2, "@aizu", "bob")
	if err != nil {
		t.Fatalf("Enqueue repo-b: %v", err)
	}

	// Both should be independently dequeuable.
	got1, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got1 == nil {
		t.Fatalf("NextPending 1: task=%v, err=%v", got1, err)
	}
	got2, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got2 == nil {
		t.Fatalf("NextPending 2: task=%v, err=%v", got2, err)
	}

	// Both tasks should be present.
	ids := map[string]bool{got1.ID: true, got2.ID: true}
	if !ids[t1.ID] || !ids[t2.ID] {
		t.Errorf("got task IDs %v, want %v", ids, map[string]bool{t1.ID: true, t2.ID: true})
	}
}

func TestQueueEnqueueWhileRunning(t *testing.T) {
	skipIfNoRedis(t)
	q := setupQueue(t)
	ctx := context.Background()

	_, err := q.Enqueue(ctx, "owner/repo", 10, 500, "@aizu", "alice")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dequeue to move to running state.
	got, err := q.NextPending(ctx, 2*time.Second)
	if err != nil || got == nil {
		t.Fatalf("NextPending: task=%v, err=%v", got, err)
	}

	// While still running, a new comment on the same issue should be deduplicated.
	task2, err := q.Enqueue(ctx, "owner/repo", 10, 501, "@aizu again", "bob")
	if err != nil {
		t.Fatalf("Enqueue while running: %v", err)
	}
	if task2 != nil {
		t.Errorf("expected nil while task is running, got %+v", task2)
	}

	// Clean up.
	q.MarkDone(ctx, got)
}
