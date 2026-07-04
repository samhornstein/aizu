// Package webhook handles GitHub webhook events and enqueues triggered tasks.
// It supports issue_comment, issues, and pull_request events.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/queue"
)

// Handler processes GitHub webhook payloads and enqueues tasks.
type Handler struct {
	cfg *config.Config
	gh  *github.Client
	q   *queue.Queue
}

// New constructs a Handler.
func New(cfg *config.Config, gh *github.Client, q *queue.Queue) *Handler {
	return &Handler{cfg: cfg, gh: gh, q: q}
}

// ServeHTTP handles incoming webhook POST requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	// Read body first (needed for signature verification).
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10 MB limit
	if err != nil {
		slog.Error("Webhook: failed to read body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify HMAC signature if a secret is configured.
	if h.cfg.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			slog.Warn("Webhook: missing X-Hub-Signature-256 header")
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return
		}
		if !verifySignature(h.cfg.WebhookSecret, body, sig) {
			slog.Warn("Webhook: invalid signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		event = "unknown"
	}

	switch event {
	case "issue_comment":
		h.handleIssueComment(w, r, body)
	case "issues":
		h.handleIssues(w, r, body)
	case "pull_request":
		h.handlePullRequest(w, r, body)
	default:
		slog.Info("Webhook: ignoring event", "event", event)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *Handler) handleIssueComment(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Action  string `json:"action"`
		Repo    Repo   `json:"repository"`
		Comment struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User User   `json:"user"`
		} `json:"comment"`
		Issue struct {
			Number int `json:"number"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Webhook: failed to parse issue_comment", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	repo := payload.Repo.FullName
	if !contains(h.cfg.Repos, repo) {
		slog.Info("Webhook: repo not watched", "repo", repo)
		w.WriteHeader(http.StatusOK)
		return
	}

	commentBody := strings.TrimSpace(payload.Comment.Body)
	if commentBody == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !h.shouldTriggerComment(repo, payload.Comment.User.Login, commentBody) {
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Info("Webhook: issue_comment trigger", "repo", repo, "number", payload.Issue.Number, "user", payload.Comment.User.Login)

	_, err := h.q.Enqueue(r.Context(), repo, payload.Issue.Number, payload.Comment.ID, commentBody, payload.Comment.User.Login)
	if err != nil {
		slog.Error("Webhook: enqueue failed (issue_comment)", "repo", repo, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// React to the comment.
	if err := h.gh.AddReaction(r.Context(), repo, payload.Comment.ID, "eyes"); err != nil {
		slog.Warn("Webhook: could not add reaction", "repo", repo, "commentID", payload.Comment.ID, "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleIssues(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Action string `json:"action"`
		Repo   Repo   `json:"repository"`
		Issue  Issue  `json:"issue"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Webhook: failed to parse issues", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	repo := payload.Repo.FullName
	if !contains(h.cfg.Repos, repo) {
		slog.Info("Webhook: repo not watched", "repo", repo)
		w.WriteHeader(http.StatusOK)
		return
	}

	if !h.shouldTriggerIssue(repo, payload.Issue.User.Login, payload.Issue.Body) {
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Info("Webhook: issues trigger", "repo", repo, "number", payload.Issue.Number, "user", payload.Issue.User.Login)

	_, err := h.q.Enqueue(r.Context(), repo, payload.Issue.Number, 0, h.cfg.Trigger, payload.Issue.User.Login)
	if err != nil {
		slog.Error("Webhook: enqueue failed (issues)", "repo", repo, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handlePullRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Action string `json:"action"`
		Repo   Repo   `json:"repository"`
		PullRequest struct {
			Number int    `json:"number"`
			Body   string `json:"body"`
			User   User   `json:"user"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Webhook: failed to parse pull_request", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	repo := payload.Repo.FullName
	if !contains(h.cfg.Repos, repo) {
		slog.Info("Webhook: repo not watched", "repo", repo)
		w.WriteHeader(http.StatusOK)
		return
	}

	if !h.shouldTriggerIssue(repo, payload.PullRequest.User.Login, payload.PullRequest.Body) {
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Info("Webhook: pull_request trigger", "repo", repo, "number", payload.PullRequest.Number, "user", payload.PullRequest.User.Login)

	_, err := h.q.Enqueue(r.Context(), repo, payload.PullRequest.Number, 0, h.cfg.Trigger, payload.PullRequest.User.Login)
	if err != nil {
		slog.Error("Webhook: enqueue failed (pull_request)", "repo", repo, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// shouldTriggerComment applies the self/keyword/allowlist filters for comments.
func (h *Handler) shouldTriggerComment(repo, author, body string) bool {
	if h.cfg.BotUsername != "" && author == h.cfg.BotUsername {
		return false
	}
	if !strings.Contains(body, h.cfg.Trigger) {
		return false
	}
	if len(h.cfg.Users) > 0 && !contains(h.cfg.Users, author) {
		slog.Info("Webhook: ignoring comment from non-allowlisted user", "repo", repo, "user", author)
		return false
	}
	return true
}

// shouldTriggerIssue applies the self/keyword/allowlist filters for issues/PRs.
func (h *Handler) shouldTriggerIssue(repo, author, body string) bool {
	if h.cfg.BotUsername != "" && author == h.cfg.BotUsername {
		return false
	}
	if !strings.Contains(body, h.cfg.Trigger) {
		return false
	}
	if len(h.cfg.Users) > 0 && !contains(h.cfg.Users, author) {
		slog.Info("Webhook: ignoring issue from non-allowlisted user", "repo", repo, "user", author)
		return false
	}
	return true
}

func verifySignature(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Repo is the subset of a GitHub repository webhook payload.
type Repo struct {
	FullName string `json:"full_name"`
}

// User is the subset of a GitHub user in a webhook payload.
type User struct {
	Login string `json:"login"`
}

// Issue is the subset of a GitHub issue in a webhook payload.
type Issue struct {
	Number int    `json:"number"`
	Body   string `json:"body"`
	User   User   `json:"user"`
}
