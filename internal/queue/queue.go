// Package queue is a Redis-backed work queue for Aizu agent tasks.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	queueKey   = "aizu:tasks"   // FIFO list of task IDs
	runningKey = "aizu:running" // set of repo#number currently executing
	queuedKey  = "aizu:queued"  // set of repo#number waiting in the queue
	failedKey  = "aizu:failed"  // dead-letter list
	taskPrefix = "aizu:task:"   // per-task JSON blob

	maxRetries = 1
	taskTTL    = 24 * time.Hour
)

// retryDelay is a var so tests can shrink the backoff.
var retryDelay = 5 * time.Second

// enqueueScript atomically checks whether the issue/PR is already active and,
// if not, stores the task JSON and appends its ID to the queue list.
// KEYS: [1]=activeKey [2]=runningKey [3]=queuedKey [4]=queueKey [5]=taskKey
// ARGV: [1]=taskID [2]=taskJSON [3]=taskTTL (seconds)
const enqueueScript = `
if redis.call('SISMEMBER', KEYS[2], KEYS[1]) == 1 or
   redis.call('SISMEMBER', KEYS[3], KEYS[1]) == 1 then
    return 0
end
redis.call('SET', KEYS[5], ARGV[2], 'EX', ARGV[3])
redis.call('LPUSH', KEYS[4], ARGV[1])
redis.call('SADD', KEYS[3], KEYS[1])
return 1
`

// Task is one unit of agent work triggered by a comment containing the trigger keyword.
type Task struct {
	ID        string `json:"id"`
	Repo      string `json:"repo"`
	Number    int    `json:"number"`
	CommentID int64  `json:"comment_id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	Retries   int    `json:"retries"`
}

// Queue wraps a Redis client.
type Queue struct {
	rdb *redis.Client
}

// New connects to Redis. An unparseable URL falls back to localhost.
func New(redisURL string) *Queue {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Error("Invalid Redis URL, falling back to localhost:6379", "url", redisURL, "error", err)
		opts = &redis.Options{Addr: "localhost:6379"}
	}
	return &Queue{rdb: redis.NewClient(opts)}
}

// Client exposes the underlying Redis client (used by the poller for "since" state).
func (q *Queue) Client() *redis.Client { return q.rdb }

func activeKey(repo string, number int) string {
	return fmt.Sprintf("%s#%d", repo, number)
}

// listPayload encodes a queue-list entry as "<taskID>|<repo#number>" so that
// NextPending can clean up the queued-set entry even when the task JSON has
// expired (the repo/number would otherwise be unknowable).
func listPayload(task *Task) string {
	return task.ID + "|" + activeKey(task.Repo, task.Number)
}

// RecoverStale clears queue state that only a previous process can have left
// behind: tasks die with the process that ran them, but their entries in the
// running set do not — and a leftover entry blocks all future triggers for
// that issue/PR. Call once at startup, before any worker goroutine consumes.
// Deleting the whole set assumes all workers live in this one process; if
// Aizu ever runs multiple worker processes against one Redis, this must
// become a per-worker lease.
func (q *Queue) RecoverStale(ctx context.Context) {
	if n, err := q.rdb.Del(ctx, runningKey).Result(); err != nil {
		slog.Warn("Could not clear running set", "error", err)
	} else if n > 0 {
		slog.Info("Cleared stale running set from previous run")
	}
}

// AllowRun increments the hourly run counter for repo and reports whether
// this run is within limit, along with the counter value (so the caller can
// reply exactly once when the limit trips). The window is a fixed UTC hour
// bucket. limit <= 0 disables the check; Redis errors fail open —
// availability over strictness here, unlike authorization.
func (q *Queue) AllowRun(ctx context.Context, repo string, limit int) (bool, int64) {
	if limit <= 0 {
		return true, 0
	}
	key := fmt.Sprintf("aizu:rate:%s:%s", repo, time.Now().UTC().Format("2006010215"))
	n, err := q.rdb.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("Could not check rate limit; allowing run", "repo", repo, "error", err)
		return true, 0
	}
	q.rdb.Expire(ctx, key, 2*time.Hour)
	return n <= int64(limit), n
}

// Enqueue atomically checks whether the issue/PR already has an active task and,
// if not, creates and queues a new one. Returns (nil, nil) if skipped because
// a task is already running or queued for that issue/PR.
func (q *Queue) Enqueue(ctx context.Context, repo string, number int, commentID int64, body, author string) (*Task, error) {
	task := &Task{
		ID:        uuid.New().String()[:8],
		Repo:      repo,
		Number:    number,
		CommentID: commentID,
		Body:      body,
		Author:    author,
	}
	data, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}
	result, err := q.rdb.Eval(ctx, enqueueScript,
		[]string{activeKey(repo, number), runningKey, queuedKey, queueKey, taskPrefix + task.ID},
		listPayload(task), string(data), int(taskTTL.Seconds()),
	).Int()
	if err != nil {
		return nil, fmt.Errorf("enqueue script: %w", err)
	}
	if result == 0 {
		return nil, nil
	}
	slog.Info("Enqueued task", "id", task.ID, "repo", repo, "number", number, "author", author)
	return task, nil
}

// push re-queues an existing task directly; used for retries after the active
// state has already been cleared.
func (q *Queue) push(ctx context.Context, task *Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	if err := q.rdb.Set(ctx, taskPrefix+task.ID, data, taskTTL).Err(); err != nil {
		return fmt.Errorf("store task: %w", err)
	}
	if err := q.rdb.LPush(ctx, queueKey, listPayload(task)).Err(); err != nil {
		return fmt.Errorf("enqueue task: %w", err)
	}
	q.rdb.SAdd(ctx, queuedKey, activeKey(task.Repo, task.Number))
	return nil
}

// NextPending blocks up to timeout for the next task and moves it from queued
// to running. Returns (nil, nil) on timeout or context cancellation.
func (q *Queue) NextPending(ctx context.Context, timeout time.Duration) (*Task, error) {
	res, err := q.rdb.BRPop(ctx, timeout, queueKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || ctx.Err() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("brpop: %w", err)
	}

	// Payload is "<taskID>|<repo#number>"; tolerate bare IDs left in Redis by
	// versions that predate the separator.
	id, active, hasActive := strings.Cut(res[1], "|")
	data, err := q.rdb.Get(ctx, taskPrefix+id).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			slog.Warn("Task expired or missing", "id", id)
			if hasActive {
				q.rdb.SRem(ctx, queuedKey, active)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("get task: %w", err)
	}

	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}

	key := activeKey(task.Repo, task.Number)
	q.rdb.SRem(ctx, queuedKey, key)
	q.rdb.SAdd(ctx, runningKey, key)
	return &task, nil
}

// MarkDone clears a successfully-processed task.
func (q *Queue) MarkDone(ctx context.Context, task *Task) {
	q.clearActive(ctx, task)
	q.rdb.Del(ctx, taskPrefix+task.ID)
	slog.Info("Task done", "id", task.ID, "repo", task.Repo, "number", task.Number)
}

// MarkFailed retries the task (with a brief backoff) if under the retry limit,
// otherwise dead-letters it. Reports whether the task was re-queued.
func (q *Queue) MarkFailed(ctx context.Context, task *Task) bool {
	q.clearActive(ctx, task)
	if task.Retries < maxRetries {
		task.Retries++
		slog.Info("Retrying task", "id", task.ID, "attempt", task.Retries+1, "repo", task.Repo, "number", task.Number)
		select {
		case <-ctx.Done():
			q.deadLetter(ctx, task)
			return false
		case <-time.After(retryDelay):
		}
		if err := q.push(ctx, task); err != nil {
			slog.Error("Failed to re-enqueue task", "id", task.ID, "error", err)
			q.deadLetter(ctx, task)
			return false
		}
		return true
	}
	q.deadLetter(ctx, task)
	return false
}

func (q *Queue) deadLetter(ctx context.Context, task *Task) {
	if data, err := json.Marshal(task); err == nil {
		q.rdb.LPush(ctx, failedKey, data)
	}
	q.rdb.Del(ctx, taskPrefix+task.ID)
	slog.Warn("Task moved to dead-letter", "id", task.ID, "repo", task.Repo, "number", task.Number, "retries", task.Retries)
}

func (q *Queue) clearActive(ctx context.Context, task *Task) {
	key := activeKey(task.Repo, task.Number)
	q.rdb.SRem(ctx, runningKey, key)
	q.rdb.SRem(ctx, queuedKey, key)
}
