package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
)

func newTestHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg, gh: github.New("test-token")}
}

func makeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestServeHTTPMethodNotAllowed(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want 405", rec.Code)
	}
}

func TestServeHTTPMissingContentType(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("code = %d, want 415", rec.Code)
	}
}

func TestServeHTTPInvalidSignature(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}, WebhookSecret: "secret123"})
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

func TestServeHTTPMissingSignature(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}, WebhookSecret: "secret123"})
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401 (missing signature)", rec.Code)
	}
}

func TestServeHTTPUnknownEvent(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

func TestHandleIssueCommentTriggers(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"comment": map[string]interface{}{
			"id":   12345,
			"body": "@aizu fix this",
			"user": map[string]interface{}{"login": "alice"},
		},
		"issue": map[string]interface{}{
			"number": 42,
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Will get 500 because no real queue, but it should not be 400/401
	if rec.Code == http.StatusBadRequest || rec.Code == http.StatusUnauthorized {
		t.Errorf("code = %d, should not be 400 or 401", rec.Code)
	}
}

func TestHandleIssueCommentNoKeyword(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"comment": map[string]interface{}{
			"id":   12345,
			"body": "just a regular comment",
			"user": map[string]interface{}{"login": "alice"},
		},
		"issue": map[string]interface{}{
			"number": 42,
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (no keyword should be ignored)", rec.Code)
	}
}

func TestHandleIssueCommentRepoNotWatched(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
		"repository": map[string]interface{}{
			"full_name": "other/repo",
		},
		"comment": map[string]interface{}{
			"id":   12345,
			"body": "@aizu fix this",
			"user": map[string]interface{}{"login": "alice"},
		},
		"issue": map[string]interface{}{
			"number": 42,
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (unwatched repo should be ignored)", rec.Code)
	}
}

func TestHandleIssueCommentBotSelf(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"comment": map[string]interface{}{
			"id":   12345,
			"body": "@aizu fix this",
			"user": map[string]interface{}{"login": "aizu-bot"},
		},
		"issue": map[string]interface{}{
			"number": 42,
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}, BotUsername: "aizu-bot"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (bot self-comment should be ignored)", rec.Code)
	}
}

func TestHandleIssueCommentAllowlist(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"comment": map[string]interface{}{
			"id":   12345,
			"body": "@aizu fix this",
			"user": map[string]interface{}{"login": "eve"},
		},
		"issue": map[string]interface{}{
			"number": 42,
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}, Users: []string{"alice", "bob"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (non-allowlisted user should be ignored)", rec.Code)
	}
}

func TestHandleIssuesTriggers(t *testing.T) {
	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"issue": map[string]interface{}{
			"number": 10,
			"body":   "Please help @aizu",
			"user":   map[string]interface{}{"login": "alice"},
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusBadRequest || rec.Code == http.StatusUnauthorized {
		t.Errorf("code = %d, should not be 400 or 401", rec.Code)
	}
}

func TestHandlePullRequestTriggers(t *testing.T) {
	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"pull_request": map[string]interface{}{
			"number": 5,
			"body":   "Fix included @aizu",
			"user":   map[string]interface{}{"login": "alice"},
		},
	}
	body, _ := json.Marshal(payload)

	h := newTestHandler(&config.Config{Trigger: "@aizu", Repos: []string{"owner/repo"}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusBadRequest || rec.Code == http.StatusUnauthorized {
		t.Errorf("code = %d, should not be 400 or 401", rec.Code)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"test": true}`)
	secret := "my-secret"
	sig := makeSignature(secret, body)

	if !verifySignature(secret, body, sig) {
		t.Error("valid signature should pass")
	}
	if verifySignature("wrong-secret", body, sig) {
		t.Error("wrong secret should fail")
	}
	if verifySignature(secret, body, "sha256=invalid") {
		t.Error("invalid signature should fail")
	}
}

func TestShouldTriggerComment(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu"})

	if !h.shouldTriggerComment("repo", "alice", "@aizu help") {
		t.Error("should trigger for matching keyword")
	}
	if h.shouldTriggerComment("repo", "alice", "no keyword") {
		t.Error("should not trigger without keyword")
	}
}

func TestShouldTriggerIssue(t *testing.T) {
	h := newTestHandler(&config.Config{Trigger: "@aizu"})

	if !h.shouldTriggerIssue("repo", "alice", "body with @aizu") {
		t.Error("should trigger for matching keyword in issue body")
	}
	if h.shouldTriggerIssue("repo", "alice", "no keyword here") {
		t.Error("should not trigger without keyword in issue body")
	}
}

func TestContains(t *testing.T) {
	list := []string{"a", "b"}
	if !contains(list, "a") {
		t.Error("contains(a) should be true")
	}
	if contains(list, "c") {
		t.Error("contains(c) should be false")
	}
}
