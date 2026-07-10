package poller

import (
	"testing"

	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/github"
)

func newTestPoller(cfg *config.Config) *Poller {
	return &Poller{cfg: cfg}
}

func TestShouldTriggerKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when comment begins with the keyword")
	}
}

func TestShouldTriggerKeywordMidComment(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "hey @aizu fix this", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when the keyword is not at the start")
	}
}

func TestShouldTriggerMissingKeyword(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	c := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}

func TestShouldTriggerIgnoresBotComment(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", BotUsername: "aizu-bot"})

	c := github.Comment{Body: "@aizu done.", User: github.User{Login: "aizu-bot"}}
	if p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = true, want false for bot's own comment")
	}
}

func TestShouldTriggerAllowlist(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", Users: []string{"alice", "bob"}})

	allowed := github.Comment{Body: "@aizu go", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", allowed) {
		t.Error("shouldTrigger() = false, want true for allowlisted user")
	}

	blocked := github.Comment{Body: "@aizu go", User: github.User{Login: "eve"}}
	if p.shouldTrigger("owner/repo", blocked) {
		t.Error("shouldTrigger() = true, want false for non-allowlisted user")
	}
}

func TestShouldTriggerEmptyAllowlistPermitsAll(t *testing.T) {
	p := newTestPoller(&config.Config{Trigger: "@aizu", Users: nil})

	c := github.Comment{Body: "@aizu help", User: github.User{Login: "anyone"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true when allowlist is empty (permit all)")
	}
}

func TestContains(t *testing.T) {
	list := []string{"alice", "bob"}
	if !contains(list, "alice") {
		t.Error("contains(alice) = false, want true")
	}
	if contains(list, "eve") {
		t.Error("contains(eve) = true, want false")
	}
	if contains(nil, "alice") {
		t.Error("contains(nil, alice) = true, want false")
	}
}

func TestShouldTriggerIssueBody(t *testing.T) {
	// The shouldTrigger function works on comments. For issue body triggers,
	// the poller uses inline checks in pollIssues instead.
	// Verify that the keyword check logic is consistent.
	p := newTestPoller(&config.Config{Trigger: "@aizu"})

	// A comment with the keyword should trigger.
	c := github.Comment{Body: "@aizu fix this", User: github.User{Login: "alice"}}
	if !p.shouldTrigger("owner/repo", c) {
		t.Error("shouldTrigger() = false, want true for matching keyword")
	}

	// A comment without the keyword should not trigger.
	c2 := github.Comment{Body: "just a regular comment", User: github.User{Login: "alice"}}
	if p.shouldTrigger("owner/repo", c2) {
		t.Error("shouldTrigger() = true, want false when keyword absent")
	}
}
