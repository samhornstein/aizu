package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommentIssueNumber(t *testing.T) {
	cases := map[string]int{
		"https://api.github.com/repos/o/r/issues/123": 123,
		"https://api.github.com/repos/o/r/issues/1":   1,
		"":          0,
		"not-a-url": 0,
	}
	for url, want := range cases {
		c := Comment{IssueURL: url}
		if got := c.IssueNumber(); got != want {
			t.Errorf("IssueNumber(%q) = %d, want %d", url, got, want)
		}
	}
}

func TestNextLink(t *testing.T) {
	cases := map[string]string{
		`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`: "https://api.github.com/x?page=2",
		`<https://api.github.com/x?page=5>; rel="last"`:                                                "",
		"": "",
	}
	for header, want := range cases {
		if got := nextLink(header); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", header, got, want)
		}
	}
}

// --- HTTP layer tests ---

// roundTripper lets tests stub the HTTP transport.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeClient(fn roundTripper) *Client {
	return &Client{token: "test-token", http: &http.Client{Transport: fn}}
}

func jsonResp(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    r,
	}
}

func TestAuthenticatedUser(t *testing.T) {
	c := fakeClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/user" {
			t.Errorf("path = %q, want /user", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header")
		}
		return jsonResp(r, 200, `{"login":"aizu-bot","type":"Bot"}`), nil
	})

	u, err := c.AuthenticatedUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Login != "aizu-bot" || u.Type != "Bot" {
		t.Errorf("User = %+v, want {aizu-bot Bot}", u)
	}
}

func TestListIssueComments(t *testing.T) {
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	comments := []Comment{
		{ID: 1, Body: "@aizu fix it", User: User{Login: "alice"}, IssueURL: "https://api.github.com/repos/o/r/issues/42"},
	}
	body, _ := json.Marshal(comments)

	c := fakeClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/repos/o/r/issues/comments" {
			t.Errorf("path = %q, want /repos/o/r/issues/comments", r.URL.Path)
		}
		if got := r.URL.Query().Get("since"); got == "" {
			t.Error("missing since query param")
		}
		return jsonResp(r, 200, string(body)), nil
	})

	got, err := c.ListIssueComments(context.Background(), "o/r", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("comments = %+v, want 1 comment with ID=1", got)
	}
}

func TestListIssueCommentsPagination(t *testing.T) {
	page1 := []Comment{{ID: 1}}
	page2 := []Comment{{ID: 2}}
	b1, _ := json.Marshal(page1)
	b2, _ := json.Marshal(page2)

	call := 0
	c := fakeClient(func(r *http.Request) (*http.Response, error) {
		call++
		resp := jsonResp(r, 200, string(b2))
		if call == 1 {
			resp = jsonResp(r, 200, string(b1))
			// Simulate a Link header pointing to page 2.
			resp.Header.Set("Link", fmt.Sprintf(`<%s?page=2>; rel="next"`, apiBase+"/repos/o/r/issues/comments"))
		}
		return resp, nil
	})

	got, err := c.ListIssueComments(context.Background(), "o/r", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("paginated result len = %d, want 2", len(got))
	}
}

func TestHTTPErrorPropagated(t *testing.T) {
	c := fakeClient(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 404, `{"message":"Not Found"}`), nil
	})

	_, err := c.GetIssue(context.Background(), "o/r", 99)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q should mention status 404", err.Error())
	}
}

func TestCreateComment(t *testing.T) {
	c := fakeClient(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/repos/o/r/issues/1/comments" {
			t.Errorf("path = %q, want /repos/o/r/issues/1/comments", r.URL.Path)
		}
		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["body"] != "hello" {
			t.Errorf("body = %q, want hello", payload["body"])
		}
		return jsonResp(r, 201, `{}`), nil
	})

	if err := c.CreateComment(context.Background(), "o/r", 1, "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestIssueIsPR(t *testing.T) {
	if (Issue{}).IsPR() {
		t.Error("zero Issue should not be a PR")
	}
	pr := Issue{PullRequest: &struct {
		URL string `json:"url"`
	}{URL: "x"}}
	if !pr.IsPR() {
		t.Error("Issue with PullRequest set should be a PR")
	}
}
