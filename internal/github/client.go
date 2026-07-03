// Package github is a small REST client for the slice of the GitHub API that
// Aizu needs: listing issue/PR comments since a timestamp, resolving whether an
// issue is a pull request, reacting to comments, and posting replies. It
// authenticates with a personal access token (PAT) only.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.github.com"

// Client talks to the GitHub REST API with a fixed bearer token.
type Client struct {
	token   string
	http    *http.Client
	baseURL string // empty = use apiBase; set in tests to redirect to a fake server
}

func (c *Client) base() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return apiBase
}

// New returns a Client. An empty token yields unauthenticated requests, which
// GitHub heavily rate-limits — a PAT is expected in practice.
func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

// NewWithBaseURL returns a Client that sends all requests to baseURL instead of
// the production GitHub API. Intended for tests only.
func NewWithBaseURL(token, baseURL string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 15 * time.Second}, baseURL: baseURL}
}

// User is the subset of a GitHub user we care about.
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// Comment is an issue or pull-request conversation comment.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	IssueURL  string    `json:"issue_url"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IssueNumber extracts the issue/PR number from the comment's issue_url, e.g.
// "https://api.github.com/repos/o/r/issues/123" -> 123.
func (c Comment) IssueNumber() int {
	i := strings.LastIndex(c.IssueURL, "/")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(c.IssueURL[i+1:])
	return n
}

// Issue is the subset of an issue/PR we care about. PullRequest is non-nil when
// the issue is actually a pull request.
type Issue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

// IsPR reports whether this issue is a pull request.
func (i Issue) IsPR() bool { return i.PullRequest != nil }

// PullRequest carries the head branch name needed to check out a PR.
type PullRequest struct {
	Number int `json:"number"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// AuthenticatedUser returns the account the token belongs to. Aizu ignores
// comments from this account to avoid reacting to its own replies.
func (c *Client) AuthenticatedUser(ctx context.Context) (User, error) {
	var u User
	err := c.get(ctx, c.base()+"/user", &u)
	return u, err
}

// ListIssueComments returns issue and PR conversation comments across the repo
// updated at or after since, oldest first, following pagination.
func (c *Client) ListIssueComments(ctx context.Context, repoFull string, since time.Time) ([]Comment, error) {
	q := url.Values{}
	q.Set("since", since.UTC().Format(time.RFC3339))
	q.Set("sort", "updated")
	q.Set("direction", "asc")
	q.Set("per_page", "100")
	next := fmt.Sprintf("%s/repos/%s/issues/comments?%s", c.base(), repoFull, q.Encode())

	var all []Comment
	for next != "" {
		var page []Comment
		link, err := c.getPaged(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		next = link
	}
	return all, nil
}

// GetIssue fetches a single issue/PR.
func (c *Client) GetIssue(ctx context.Context, repoFull string, number int) (*Issue, error) {
	var i Issue
	if err := c.get(ctx, fmt.Sprintf("%s/repos/%s/issues/%d", c.base(), repoFull, number), &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// GetPullRequest fetches a single pull request (for its head branch).
func (c *Client) GetPullRequest(ctx context.Context, repoFull string, number int) (*PullRequest, error) {
	var pr PullRequest
	if err := c.get(ctx, fmt.Sprintf("%s/repos/%s/pulls/%d", c.base(), repoFull, number), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// AddReaction adds a reaction (e.g. "eyes") to an issue comment.
func (c *Client) AddReaction(ctx context.Context, repoFull string, commentID int64, content string) error {
	body := map[string]string{"content": content}
	u := fmt.Sprintf("%s/repos/%s/issues/comments/%d/reactions", c.base(), repoFull, commentID)
	return c.post(ctx, u, body, nil)
}

// CreateComment posts a comment on an issue or PR.
func (c *Client) CreateComment(ctx context.Context, repoFull string, number int, body string) error {
	u := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.base(), repoFull, number)
	return c.post(ctx, u, map[string]string{"body": body}, nil)
}

// --- low-level helpers ---

func (c *Client) get(ctx context.Context, url string, out any) error {
	_, err := c.getPaged(ctx, url, out)
	return err
}

// getPaged performs a GET and returns the rel="next" URL from the Link header,
// if any, so callers can follow pagination.
func (c *Client) getPaged(ctx context.Context, url string, out any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := decode(resp, out); err != nil {
		return "", err
	}
	return nextLink(resp.Header.Get("Link")), nil
}

func (c *Client) post(ctx context.Context, url string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return decode(resp, out)
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

func decode(resp *http.Response, out any) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github: %s -> %d: %s", resp.Request.URL.Path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// nextLink parses a GitHub Link header and returns the rel="next" URL, if present.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(strings.TrimSpace(part), ";")
		if len(segs) < 2 {
			continue
		}
		urlPart := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(urlPart, "<") || !strings.HasSuffix(urlPart, ">") {
			continue
		}
		for _, s := range segs[1:] {
			if strings.TrimSpace(s) == `rel="next"` {
				return urlPart[1 : len(urlPart)-1]
			}
		}
	}
	return ""
}
