// Package worker pulls tasks from the queue and runs the agent for each one:
// react to the triggering comment, run the engine in a sandbox, and post the
// result back to GitHub.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/samhornstein/aizu/internal/executor"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
)

// Worker processes one task at a time from the shared queue.
type Worker struct {
	q      *queue.Queue
	exec   executor.Executor
	gh     *github.Client
	loader *template.Loader
}

// New constructs a Worker.
func New(q *queue.Queue, exec executor.Executor, gh *github.Client, loader *template.Loader) *Worker {
	return &Worker{q: q, exec: exec, gh: gh, loader: loader}
}

// Run loops until ctx is cancelled, processing tasks as they arrive.
func (w *Worker) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		task, err := w.q.NextPending(ctx, 5*time.Second)
		if err != nil {
			slog.Error("Queue read failed", "error", err)
			continue
		}
		if task == nil {
			continue
		}
		w.process(ctx, task)
	}
}

func (w *Worker) process(ctx context.Context, task *queue.Task) {
	log := slog.With("id", task.ID, "repo", task.Repo, "number", task.Number)
	log.Info("Processing task")

	if err := w.gh.AddReaction(ctx, task.Repo, task.CommentID, "eyes"); err != nil {
		log.Warn("Could not add reaction", "error", err)
	}

	output, err := w.handle(ctx, task, log)
	if err != nil {
		log.Error("Task failed", "error", err)
		if !w.q.MarkFailed(ctx, task) {
			w.reply(ctx, task, fmt.Sprintf("Aizu failed to process this request: %v", err), log)
		}
		return
	}

	w.reply(ctx, task, output, log)
	w.q.MarkDone(ctx, task)
}

// handle runs the agent in a sandbox and returns the message to post back.
func (w *Worker) handle(ctx context.Context, task *queue.Task, log *slog.Logger) (string, error) {
	issue, err := w.gh.GetIssue(ctx, task.Repo, task.Number)
	if err != nil {
		return "", fmt.Errorf("fetch issue: %w", err)
	}

	branch := ""
	if issue.IsPR() {
		pr, err := w.gh.GetPullRequest(ctx, task.Repo, task.Number)
		if err != nil {
			return "", fmt.Errorf("fetch pull request: %w", err)
		}
		branch = pr.Head.Ref
	}

	sid, err := w.exec.Create(task.Repo, branch)
	if err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}
	defer w.exec.Destroy(sid)

	prompt := w.buildPrompt(sid, issue, task)
	exitCode, output, err := w.exec.RunEngine(sid, prompt)
	if err != nil {
		return "", fmt.Errorf("run engine: %w", err)
	}
	log.Info("Engine finished", "exit_code", exitCode)

	if exitCode != 0 {
		return "", fmt.Errorf("engine exited %d:\n%s", exitCode, truncate(output, 1000))
	}
	return formatResult(output), nil
}

// buildPrompt assembles the instructions and GitHub context into the engine prompt.
func (w *Worker) buildPrompt(sid string, issue *github.Issue, task *queue.Task) string {
	kind := "issue"
	if issue.IsPR() {
		kind = "pull request"
	}
	var b strings.Builder
	b.WriteString(w.loader.Resolve(w.exec, sid))
	b.WriteString("\n\n---\n\n")
	fmt.Fprintf(&b, "You are responding to a comment on %s #%d (a %s) in %s.\n\n", kind, issue.Number, kind, task.Repo)
	fmt.Fprintf(&b, "Title: %s\n", issue.Title)
	if body := strings.TrimSpace(issue.Body); body != "" {
		fmt.Fprintf(&b, "Description:\n%s\n", truncate(body, 4000))
	}
	fmt.Fprintf(&b, "\nThe comment from @%s that triggered you:\n%s\n", task.Author, task.Body)
	return b.String()
}

func (w *Worker) reply(ctx context.Context, task *queue.Task, body string, log *slog.Logger) {
	if err := w.gh.CreateComment(ctx, task.Repo, task.Number, body); err != nil {
		log.Warn("Could not post reply", "error", err)
	}
}

func formatResult(output string) string {
	out := strings.TrimSpace(output)
	if out == "" {
		out = "Done."
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)"
}
