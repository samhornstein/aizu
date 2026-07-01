package executor

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/samhornstein/aizu/internal/config"
)

// skipIfNoDocker skips the test if Docker is not available.
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found, skipping integration test")
	}
	// Check if Docker daemon is running.
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("docker daemon not reachable, skipping integration test")
	}
}

// testImage returns a lightweight Docker image for testing.
func testImage() string {
	if v := os.Getenv("TEST_CONTAINER_IMAGE"); v != "" {
		return v
	}
	return "alpine:3.20"
}

// testExecutor creates an executor configured for testing.
func testExecutor(t *testing.T) *containerExecutor {
	t.Helper()
	return &containerExecutor{cfg: &config.Config{
		ContainerImage: testImage(),
		Timeout:        30,
	}}
}

func TestContainerCreateAndDestroy(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	// Use a public repo that's small and fast to clone.
	repo := "docker-library/official-images"
	sid, err := e.Create(repo, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer e.Destroy(sid)

	if sid == "" {
		t.Fatal("Create returned empty sid")
	}

	// Verify the container exists.
	out, err := run("docker inspect -f '{{.Name}}' "+sid, 5*time.Second)
	if err != nil {
		t.Fatalf("container not found: %v", err)
	}
	if out[:len(out)-1] != "/"+sid { // trim newline
		t.Errorf("container name = %q, want /%s", out, sid)
	}

	// Verify the repo was cloned.
	out, err = e.exec(sid, "ls /workspace/repo/README.md", 5*time.Second)
	if err != nil {
		t.Fatalf("repo not cloned: %v", err)
	}
}

func TestContainerCreateWithBranch(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	// Use a repo with a known branch.
	sid, err := e.Create("docker-library/official-images", "main")
	if err != nil {
		t.Fatalf("Create with branch: %v", err)
	}
	defer e.Destroy(sid)

	// Verify we're on the right branch.
	out, err := e.exec(sid, "cd /workspace/repo && git rev-parse --abbrev-ref HEAD", 5*time.Second)
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	if out[:len(out)-1] != "main" {
		t.Errorf("branch = %q, want main", out[:len(out)-1])
	}
}

func TestContainerRunEngine(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	sid, err := e.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer e.Destroy(sid)

	// Run a simple command as the "engine".
	e.cfg.EngineCommand = "echo 'hello from agent'"
	exitCode, output, err := e.RunEngine(sid, "test prompt")
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if output[:len(output)-1] != "hello from agent" {
		t.Errorf("output = %q, want 'hello from agent'", output)
	}
}

func TestContainerRunEngineTimeout(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)
	e.cfg.Timeout = 2 // 2 second timeout
	e.cfg.EngineCommand = "sleep 30"

	sid, err := e.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer e.Destroy(sid)

	exitCode, output, err := e.RunEngine(sid, "test prompt")
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if exitCode != 124 {
		t.Errorf("exit code = %d, want 124 (timeout)", exitCode)
	}
	if len(output) == 0 {
		t.Error("expected timeout message in output")
	}
}

func TestContainerReadFile(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	sid, err := e.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer e.Destroy(sid)

	// Write a test file.
	_, err = e.exec(sid, "echo 'test content' > /tmp/test.txt", 5*time.Second)
	if err != nil {
		t.Fatalf("exec write: %v", err)
	}

	content, err := e.ReadFile(sid, "/tmp/test.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if content[:len(content)-1] != "test content" {
		t.Errorf("ReadFile = %q, want 'test content'", content)
	}
}

func TestContainerReadFileNotFound(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	sid, err := e.Create("docker-library/official-images", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer e.Destroy(sid)

	_, err = e.ReadFile(sid, "/nonexistent/file")
	if err == nil {
		t.Error("expected error reading nonexistent file")
	}
}

func TestCleanupStale(t *testing.T) {
	skipIfNoDocker(t)
	e := testExecutor(t)

	// Create a stale container manually.
	staleName := "aizu-stale-test"
	_, err := run("docker run -d --name="+staleName+" --label=aizu=true "+testImage()+" sleep infinity", 10*time.Second)
	if err != nil {
		t.Fatalf("create stale container: %v", err)
	}
	defer func() { _, _ = run("docker rm -f "+staleName, 5*time.Second) }()

	// Cleanup should remove it.
	e.CleanupStale()

	// Verify it's gone.
	_, err = run("docker inspect "+staleName, 5*time.Second)
	if err == nil {
		t.Error("stale container should have been removed")
	}
}
