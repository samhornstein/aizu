// Package template resolves the agent's system instructions. A repository can
// override the built-in default by committing .aizu/AIZU.md.
package template

import "strings"

const repoInstructionsPath = "/workspace/repo/.aizu/AIZU.md"

// Loader holds the default instructions embedded in the binary.
type Loader struct {
	defaultInstructions string
}

// NewLoader returns a Loader with the given default instructions.
func NewLoader(def string) *Loader {
	return &Loader{defaultInstructions: def}
}

// fileReader reads a path from inside a sandbox (satisfied by executor.Executor).
type fileReader interface {
	ReadFile(sid, path string) (string, error)
}

// Resolve returns the repo's .aizu/AIZU.md if present in the sandbox, otherwise
// the built-in default.
func (l *Loader) Resolve(r fileReader, sid string) string {
	if content, err := r.ReadFile(sid, repoInstructionsPath); err == nil {
		if s := strings.TrimSpace(content); s != "" {
			return s
		}
	}
	return l.defaultInstructions
}
