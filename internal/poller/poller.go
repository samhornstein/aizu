// Package poller periodically polls GitHub for new issue/PR comments and
// enqueues a task for each comment that begins with the trigger keyword.
// It also polls for issues whose body begins with the trigger keyword.
//
// Per-repo "since" state lives in Redis.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
)

const sincePrefix = "aizu:since:"
const issuesSincePrefix = "aizu:issuesSince:"

// Seen-markers record triggers that have already been enqueued, so neither
// comment edits nor issue updates (including our own reply bumping the
// issue's updated_at) can re-trigger the agent. The queue's queued/running
// dedupe only guards while a task is active; these markers are what make a
// trigger fire exactly once.
const seenIssuePrefix = "aizu:seen:issue:"
const seenCommentPrefix = "aizu:seen:comment:"
const seenTTL = 30 * 24 * time.Hour

// Poller polls GitHub and enqueues triggered comments.
type Poller struct {
	cfg *config.Config
	gh  *github.Client
	q   *queue.Queue
	rdb *redis.Client
}

// New constructs a Poller.
func New(cfg *config.Config, gh *github.Client, q *queue.Queue) *Poller {
	return &Poller{cfg: cfg, gh: gh, q: q, rdb: q.Client()}
}

// Run polls on the configured interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("Poller started", "interval", p.cfg.PollInterval, "trigger", p.cfg.Trigger)
	p.pollOnce(ctx)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Poller stopped")
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	for _, repo := range p.cfg.Repos {
		if err := p.pollRepo(ctx, repo); err != nil {
			slog.Error("Poll failed", "repo", repo, "error", err)
		}
	}
}

func (p *Poller) pollRepo(ctx context.Context, repo string) error {
	// Capture the cutoff before the request so anything created during this
	// cycle is picked up next time rather than missed.
	pollStart := time.Now()
	since := p.lastSince(ctx, repo)

	comments, err := p.gh.ListIssueComments(ctx, repo, since)
	if err != nil {
		return err
	}

	for _, c := range comments {
		if !p.shouldTrigger(repo, c) {
			continue
		}
		number := c.IssueNumber()
		if number == 0 {
			slog.Warn("Could not parse issue number", "repo", repo, "issue_url", c.IssueURL)
			continue
		}
		seenKey := fmt.Sprintf("%s%s#%d", seenCommentPrefix, repo, c.ID)
		if p.alreadySeen(ctx, seenKey) {
			continue // handled before; an edit to the comment must not re-run
		}
		if _, err := p.q.Enqueue(ctx, repo, number, c.ID, c.Body, c.User.Login); err != nil {
			// Clear the marker so the next poll retries this trigger.
			p.rdb.Del(ctx, seenKey)
			slog.Error("Enqueue failed", "repo", repo, "number", number, "error", err)
		}
	}

	p.saveSince(ctx, repo, pollStart)

	// Also poll for issues with the trigger keyword in the body.
	if err := p.pollIssues(ctx, repo); err != nil {
		return err
	}

	return nil
}

// pollIssues checks for issues (including PRs) whose body begins with the
// trigger keyword and enqueues them.
func (p *Poller) pollIssues(ctx context.Context, repo string) error {
	issuesSince := p.lastIssuesSince(ctx, repo)

	issues, err := p.gh.ListIssues(ctx, repo, issuesSince)
	if err != nil {
		return err
	}

	for _, issue := range issues {
		if strings.Contains(issue.Body, github.ReplyMarker) {
			continue // body written by Aizu itself
		}
		if !p.triggered(issue.Body) {
			continue
		}
		if len(p.cfg.Users) > 0 && !contains(p.cfg.Users, issue.User.Login) {
			slog.Info("Ignoring issue from non-allowlisted user", "repo", repo, "user", issue.User.Login)
			continue
		}
		seenKey := fmt.Sprintf("%s%s#%d", seenIssuePrefix, repo, issue.Number)
		if p.alreadySeen(ctx, seenKey) {
			continue // an issue body triggers exactly once; comment to re-run
		}
		// Enqueue with CommentID=0 to signal this was triggered by the issue body.
		if _, err := p.q.Enqueue(ctx, repo, issue.Number, 0, p.cfg.Trigger, issue.User.Login); err != nil {
			// Clear the marker so the next poll retries this trigger.
			p.rdb.Del(ctx, seenKey)
			slog.Error("Enqueue failed (issue body)", "repo", repo, "number", issue.Number, "error", err)
		}
	}

	if len(issues) > 0 {
		// Advance the cursor just past the latest issue's updated time:
		// GitHub's `since` is inclusive, so saving the exact timestamp would
		// refetch the newest issue every poll. Correctness doesn't depend on
		// this (the seen-markers do that); it only avoids refetch churn.
		latest := issues[len(issues)-1].UpdatedAt
		p.saveIssuesSince(ctx, repo, latest.Add(time.Second))
	}

	return nil
}

func (p *Poller) lastIssuesSince(ctx context.Context, repo string) time.Time {
	v, err := p.rdb.Get(ctx, issuesSincePrefix+repo).Result()
	if err != nil {
		// First run for this repo: only consider issues from now on.
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Now()
	}
	return t
}

func (p *Poller) saveIssuesSince(ctx context.Context, repo string, t time.Time) {
	if err := p.rdb.Set(ctx, issuesSincePrefix+repo, t.UTC().Format(time.RFC3339), 0).Err(); err != nil {
		slog.Warn("Could not persist issues poll cursor", "repo", repo, "error", err)
	}
}

// alreadySeen atomically records key if unseen and reports whether it was
// already recorded. Redis errors fail open: the worst case is a duplicate
// run, which beats silently dropping a trigger.
func (p *Poller) alreadySeen(ctx context.Context, key string) bool {
	ok, err := p.rdb.SetNX(ctx, key, "1", seenTTL).Result()
	if err != nil {
		slog.Warn("Could not check seen-marker; proceeding", "key", key, "error", err)
		return false
	}
	return !ok
}

// triggered reports whether text begins with the trigger keyword, ignoring
// leading whitespace. Requiring the keyword at the start (rather than anywhere
// in the text) avoids false triggers from incidental mentions.
func (p *Poller) triggered(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), p.cfg.Trigger)
}

// shouldTrigger applies the self/keyword/allowlist filters. Aizu's own
// replies are recognized by the ReplyMarker they carry, not by author, so a
// personal token (whose login equals the triggering user's) works.
func (p *Poller) shouldTrigger(repo string, c github.Comment) bool {
	if strings.Contains(c.Body, github.ReplyMarker) {
		return false // one of our own replies
	}
	if !p.triggered(c.Body) {
		return false
	}
	if len(p.cfg.Users) > 0 && !contains(p.cfg.Users, c.User.Login) {
		slog.Info("Ignoring comment from non-allowlisted user", "repo", repo, "user", c.User.Login)
		return false
	}
	return true
}

func (p *Poller) lastSince(ctx context.Context, repo string) time.Time {
	v, err := p.rdb.Get(ctx, sincePrefix+repo).Result()
	if err != nil {
		// First run for this repo: only consider comments from now on.
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Now()
	}
	return t
}

func (p *Poller) saveSince(ctx context.Context, repo string, t time.Time) {
	if err := p.rdb.Set(ctx, sincePrefix+repo, t.UTC().Format(time.RFC3339), 0).Err(); err != nil {
		slog.Warn("Could not persist poll cursor", "repo", repo, "error", err)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
