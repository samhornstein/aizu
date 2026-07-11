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

	"github.com/samhornstein/aizu/internal/config"
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

	// secrets are masked in everything posted publicly; the agent has them
	// in its sandbox and engine output may echo them.
	secrets []string
}

// New constructs a Worker.
func New(q *queue.Queue, exec executor.Executor, gh *github.Client, loader *template.Loader, cfg *config.Config) *Worker {
	return &Worker{
		q:       q,
		exec:    exec,
		gh:      gh,
		loader:  loader,
		secrets: []string{cfg.GitHubToken, cfg.AnthropicKey, cfg.OpenAIKey},
	}
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

	// Only react to the comment if this was triggered by a comment.
	// Issue-body triggers have CommentID=0.
	if task.CommentID > 0 {
		if err := w.gh.AddReaction(ctx, task.Repo, task.CommentID, "eyes"); err != nil {
			log.Warn("Could not add reaction", "error", err)
		}
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
	prNumber := 0
	isFork := false
	if issue.IsPR() {
		pr, err := w.gh.GetPullRequest(ctx, task.Repo, task.Number)
		if err != nil {
			return "", fmt.Errorf("fetch pull request: %w", err)
		}
		branch = pr.Head.Ref
		prNumber = pr.Number
		// An empty FullName means the head repo is gone (deleted fork) —
		// unpushable either way.
		isFork = pr.Head.Repo.FullName == "" || pr.Head.Repo.FullName != task.Repo
	}

	sid, err := w.exec.Create(task.Repo, branch, prNumber)
	if err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}
	defer w.exec.Destroy(sid)

	prompt := w.buildPrompt(sid, issue, task, isFork)
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

// buildPrompt assembles the instructions and GitHub context into the engine
// prompt. isFork marks PRs whose head lives in another repo, which the bot
// token cannot push to.
func (w *Worker) buildPrompt(sid string, issue *github.Issue, task *queue.Task, isFork bool) string {
	kind := "issue"
	if issue.IsPR() {
		kind = "pull request"
	}
	var b strings.Builder
	b.WriteString(w.loader.Resolve(w.exec, sid))
	b.WriteString("\n\n---\n\n")

	if task.CommentID > 0 {
		// Triggered by a comment.
		fmt.Fprintf(&b, "You are responding to a comment on %s #%d (a %s) in %s.\n\n", kind, issue.Number, kind, task.Repo)
		fmt.Fprintf(&b, "Title: %s\n", issue.Title)
		if body := strings.TrimSpace(issue.Body); body != "" {
			fmt.Fprintf(&b, "Description:\n%s\n", truncate(body, 4000))
		}
		fmt.Fprintf(&b, "\nThe comment from @%s that triggered you:\n%s\n", task.Author, task.Body)
	} else {
		// Triggered by the issue body itself.
		fmt.Fprintf(&b, "You are responding to %s #%d (a %s) in %s.\n\n", kind, issue.Number, kind, task.Repo)
		fmt.Fprintf(&b, "Title: %s\n", issue.Title)
		fmt.Fprintf(&b, "The request from @%s in the %s body:\n", task.Author, kind)
		fmt.Fprintf(&b, "%s\n", truncate(strings.TrimSpace(issue.Body), 4000))
	}
	if issue.IsPR() && isFork {
		b.WriteString("\nNote: this pull request comes from a fork. You cannot push to its branch. Do not attempt to push; instead, describe the changes you would make, or include a patch in your reply.\n")
	}
	return b.String()
}

// reply is the single choke point through which success and failure comments
// flow; everything posted publicly is redacted here.
func (w *Worker) reply(ctx context.Context, task *queue.Task, body string, log *slog.Logger) {
	if err := w.gh.CreateComment(ctx, task.Repo, task.Number, redact(body, w.secrets...)); err != nil {
		log.Warn("Could not post reply", "error", err)
	}
}

// redact masks configured secrets in text before it is posted publicly.
func redact(text string, secrets ...string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[redacted]")
		}
	}
	return text
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
