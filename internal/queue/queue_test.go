package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestQueue(t *testing.T) (*Queue, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return New("redis://" + mr.Addr()), mr
}

// TestRecoverStaleUnblocksIssue: a task that was running when the process
// died must not block the issue forever — RecoverStale clears the running
// set so a new trigger can enqueue.
func TestRecoverStaleUnblocksIssue(t *testing.T) {
	q, mr := newTestQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, "o/r", 1, 100, "@aizu go", "alice"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if task, err := q.NextPending(ctx, time.Second); err != nil || task == nil {
		t.Fatalf("NextPending: task=%v err=%v", task, err)
	}

	// Simulate a crash mid-task: a fresh process connects to the same Redis.
	q2 := New("redis://" + mr.Addr())

	dup, err := q2.Enqueue(ctx, "o/r", 1, 101, "@aizu again", "alice")
	if err != nil {
		t.Fatalf("Enqueue while stale-running: %v", err)
	}
	if dup != nil {
		t.Fatal("Enqueue succeeded while issue is in the stale running set, want skip")
	}

	q2.RecoverStale(ctx)

	task, err := q2.Enqueue(ctx, "o/r", 1, 101, "@aizu again", "alice")
	if err != nil {
		t.Fatalf("Enqueue after RecoverStale: %v", err)
	}
	if task == nil {
		t.Error("Enqueue skipped after RecoverStale, want success (issue unblocked)")
	}
}

// TestExpiredTaskCleansQueuedSet: when a task's JSON has expired, NextPending
// must remove the leftover queued-set entry so the issue isn't blocked.
func TestExpiredTaskCleansQueuedSet(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()

	task, err := q.Enqueue(ctx, "o/r", 2, 100, "@aizu go", "alice")
	if err != nil || task == nil {
		t.Fatalf("Enqueue: task=%v err=%v", task, err)
	}
	// Expire the task JSON out from under the queue.
	if err := q.Client().Del(ctx, taskPrefix+task.ID).Err(); err != nil {
		t.Fatalf("del task JSON: %v", err)
	}

	got, err := q.NextPending(ctx, time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if got != nil {
		t.Fatalf("NextPending returned %+v for expired task, want nil", got)
	}

	if q.Client().SIsMember(ctx, queuedKey, activeKey("o/r", 2)).Val() {
		t.Error("queued set still contains the expired task's issue, want removed")
	}
	retry, err := q.Enqueue(ctx, "o/r", 2, 101, "@aizu retry", "alice")
	if err != nil || retry == nil {
		t.Errorf("Enqueue after expired-task cleanup: task=%v err=%v, want success", retry, err)
	}
}

// TestEnqueueRoundTrip: fields survive the queue, and MarkDone clears all
// active state so the issue can trigger again.
func TestEnqueueRoundTrip(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, "o/r", 3, 555, "@aizu build", "bob"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	task, err := q.NextPending(ctx, time.Second)
	if err != nil || task == nil {
		t.Fatalf("NextPending: task=%v err=%v", task, err)
	}
	if task.Repo != "o/r" || task.Number != 3 || task.CommentID != 555 ||
		task.Body != "@aizu build" || task.Author != "bob" || task.Retries != 0 {
		t.Errorf("task round-trip mismatch: %+v", task)
	}

	q.MarkDone(ctx, task)
	for _, set := range []string{queuedKey, runningKey} {
		if q.Client().SIsMember(ctx, set, activeKey("o/r", 3)).Val() {
			t.Errorf("%s still contains issue after MarkDone", set)
		}
	}
	if again, err := q.Enqueue(ctx, "o/r", 3, 556, "@aizu more", "bob"); err != nil || again == nil {
		t.Errorf("Enqueue after MarkDone: task=%v err=%v, want success", again, err)
	}
}

// TestOldFormatListEntry: a bare task ID (no "|" separator) left in the list
// by a pre-upgrade version must still be processed.
func TestOldFormatListEntry(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()

	task := &Task{ID: "abcd1234", Repo: "o/r", Number: 4, CommentID: 9, Body: "@aizu go", Author: "alice"}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Client().Set(ctx, taskPrefix+task.ID, data, taskTTL).Err(); err != nil {
		t.Fatalf("set task JSON: %v", err)
	}
	if err := q.Client().LPush(ctx, queueKey, task.ID).Err(); err != nil {
		t.Fatalf("lpush bare id: %v", err)
	}

	got, err := q.NextPending(ctx, time.Second)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if got == nil || got.ID != task.ID || got.Number != 4 {
		t.Errorf("NextPending = %+v, want old-format task %s", got, task.ID)
	}
}

// TestAllowRunLimits: the counter admits exactly `limit` runs per hour
// bucket, then denies with an increasing count; expiry resets the window.
func TestAllowRunLimits(t *testing.T) {
	q, mr := newTestQueue(t)
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		ok, n := q.AllowRun(ctx, "o/r", 2)
		if !ok || n != int64(i) {
			t.Fatalf("run %d: AllowRun = (%v, %d), want (true, %d)", i, ok, n, i)
		}
	}
	if ok, n := q.AllowRun(ctx, "o/r", 2); ok || n != 3 {
		t.Errorf("third run: AllowRun = (%v, %d), want (false, 3)", ok, n)
	}

	mr.FastForward(2 * time.Hour)
	if ok, _ := q.AllowRun(ctx, "o/r", 2); !ok {
		t.Error("counter should expire after the window")
	}
}

func TestAllowRunDisabled(t *testing.T) {
	q, _ := newTestQueue(t)
	for i := 0; i < 5; i++ {
		if ok, _ := q.AllowRun(context.Background(), "o/r", 0); !ok {
			t.Fatal("limit 0 must disable the check")
		}
	}
}
