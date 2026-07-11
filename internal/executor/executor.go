// Package executor runs an agent against a repository inside an isolated Docker
// container: clone, check out the PR branch (if any), run the engine, tear down.
package executor

import "github.com/samhornstein/aizu/internal/config"

// Executor creates a sandbox, runs the agent engine in it, and destroys it.
type Executor interface {
	// Create clones repo into a fresh container, returning a sandbox id.
	// For PR tasks prNumber is the pull request number: its head is fetched
	// via refs/pull/<n>/head (which exists in the base repo even for fork
	// PRs) and checked out as branch. prNumber 0 with a non-empty branch
	// checks out that branch directly.
	Create(repo, branch string, prNumber int) (sid string, err error)
	// RunEngine runs the configured engine command with the given prompt,
	// returning its exit code and combined output.
	RunEngine(sid, prompt string) (exitCode int, output string, err error)
	// ReadFile returns the contents of a path inside the sandbox.
	ReadFile(sid, path string) (string, error)
	// Destroy removes the sandbox.
	Destroy(sid string)
	// CleanupStale removes leftover Aizu containers from previous runs.
	CleanupStale()
}

// New returns the default (Docker container) executor.
func New(cfg *config.Config) Executor {
	return &containerExecutor{cfg: cfg, models: make(map[string]string)}
}
