package template

import (
	"errors"
	"testing"
)

type mockReader struct {
	content string
	err     error
}

func (m *mockReader) ReadFile(sid, path string) (string, error) {
	return m.content, m.err
}

func TestResolveUsesRepoFile(t *testing.T) {
	loader := NewLoader("default instructions")
	got := loader.Resolve(&mockReader{content: "repo-specific instructions"}, "sid-1")
	if got != "repo-specific instructions" {
		t.Errorf("Resolve() = %q, want repo-specific instructions", got)
	}
}

func TestResolveFallsBackOnReadError(t *testing.T) {
	loader := NewLoader("default instructions")
	got := loader.Resolve(&mockReader{err: errors.New("not found")}, "sid-1")
	if got != "default instructions" {
		t.Errorf("Resolve() = %q, want default instructions", got)
	}
}

func TestResolveFallsBackOnEmptyFile(t *testing.T) {
	loader := NewLoader("default instructions")
	for _, content := range []string{"", "   ", "\n\t\n"} {
		got := loader.Resolve(&mockReader{content: content}, "sid-1")
		if got != "default instructions" {
			t.Errorf("Resolve(empty content %q) = %q, want default instructions", content, got)
		}
	}
}
