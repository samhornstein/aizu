// Package worker pulls tasks from the queue and runs the agent for each one:
// react to the triggering comment, run the engine in a sandbox, and post the
// result back to GitHub.
package worker

import (
	"context"
	"errors"
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
	cfg    *config.Config

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
		cfg:     cfg,
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

	w.react(ctx, task, "eyes", log)

	// Help requests are answered by the worker itself — no sandbox, no
	// model tokens. Only comment triggers qualify: an issue-body trigger's
	// task body is just the trigger word (the real request is in the issue).
	if task.CommentID > 0 && isHelpRequest(task.Body, w.cfg.Trigger) {
		w.reply(ctx, task, 0, helpText(w.cfg.Trigger), log)
		w.q.MarkDone(ctx, task)
		return
	}

	if ok, n := w.q.AllowRun(ctx, task.Repo, w.cfg.MaxRunsPerHour); !ok {
		// Reply exactly once per window — on the first excess trigger.
		if n == int64(w.cfg.MaxRunsPerHour)+1 {
			until := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
			w.reply(ctx, task, 0, fmt.Sprintf("Aizu's rate limit for this repo is reached (%d runs/hour). Try again after %s UTC.",
				w.cfg.MaxRunsPerHour, until.Format("15:04")), log)
		}
		log.Warn("Rate limit reached; dropping task", "count", n)
		w.q.MarkDone(ctx, task)
		return
	}

	// Instant acknowledgment in the thread; edited into the final result so
	// there is exactly one outcome comment.
	var placeholderID int64
	if id, err := w.gh.CreateComment(ctx, task.Repo, task.Number, "⏳ Aizu is working on this…"); err != nil {
		log.Warn("Could not post progress comment", "error", err)
	} else {
		placeholderID = id
	}

	output, err := w.handle(ctx, task, log)
	if errors.Is(err, errClosedIssue) {
		// A normal outcome, not a failure: no retry, no 😕. The placeholder
		// is already up, so edit it rather than leave "working on this…".
		log.Info("Issue is closed; skipping")
		w.reply(ctx, task, placeholderID, "This issue is closed — Aizu skipped it. Reopen it and trigger again to run.", log)
		w.q.MarkDone(ctx, task)
		return
	}
	if err != nil {
		log.Error("Task failed", "error", err)
		if !w.q.MarkFailed(ctx, task) {
			w.reply(ctx, task, placeholderID, fmt.Sprintf("Aizu failed to process this request: %v", err), log)
			w.react(ctx, task, "confused", log)
		}
		return
	}

	w.reply(ctx, task, placeholderID, output, log)
	w.react(ctx, task, "rocket", log)
	w.q.MarkDone(ctx, task)
}

// react adds a reaction to the triggering comment, if there is one
// (issue-body triggers have CommentID=0).
func (w *Worker) react(ctx context.Context, task *queue.Task, content string, log *slog.Logger) {
	if task.CommentID == 0 {
		return
	}
	if err := w.gh.AddReaction(ctx, task.Repo, task.CommentID, content); err != nil {
		log.Warn("Could not add reaction", "content", content, "error", err)
	}
}

// isHelpRequest reports whether body is the bare trigger word or the trigger
// word followed only by "help".
func isHelpRequest(body, trigger string) bool {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), trigger))
	return rest == "" || strings.EqualFold(rest, "help")
}

func helpText(trigger string) string {
	return fmt.Sprintf("**Aizu** runs a coding agent on this repository, on its operator's machine.\n\n"+
		"Start a comment (or an issue body) with `%[1]s` and describe what you want:\n\n"+
		"- `%[1]s implement this` — work the issue and open a pull request\n"+
		"- `%[1]s review this PR` — review the pull request it's commented on\n"+
		"- `%[1]s help` — this message\n\n"+
		"Aizu reacts with 👀 when it picks the task up, posts a progress comment, and edits it into the result (🚀 done, 😕 failed).",
		trigger)
}

// errClosedIssue marks a task skipped because its issue/PR is closed; it is
// a normal outcome, not a failure.
var errClosedIssue = errors.New("issue is closed")

// handle runs the agent in a sandbox and returns the message to post back.
func (w *Worker) handle(ctx context.Context, task *queue.Task, log *slog.Logger) (string, error) {
	issue, err := w.gh.GetIssue(ctx, task.Repo, task.Number)
	if err != nil {
		return "", fmt.Errorf("fetch issue: %w", err)
	}
	if issue.State == "closed" {
		return "", errClosedIssue
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
// flow; everything posted publicly is redacted here. With a placeholder
// comment ID it edits that comment in place, falling back to a new comment
// on error — a result must never be lost.
func (w *Worker) reply(ctx context.Context, task *queue.Task, placeholderID int64, body string, log *slog.Logger) {
	body = redact(body, w.secrets...)
	if placeholderID > 0 {
		err := w.gh.UpdateComment(ctx, task.Repo, placeholderID, body)
		if err == nil {
			return
		}
		log.Warn("Could not update progress comment; posting a new one", "error", err)
	}
	if _, err := w.gh.CreateComment(ctx, task.Repo, task.Number, body); err != nil {
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
