package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/samhornstein/aizu/internal/config"
	"github.com/samhornstein/aizu/internal/executor"
	"github.com/samhornstein/aizu/internal/github"
	"github.com/samhornstein/aizu/internal/poller"
	"github.com/samhornstein/aizu/internal/queue"
	"github.com/samhornstein/aizu/internal/template"
	"github.com/samhornstein/aizu/internal/worker"
)

//go:embed AIZU.md
var defaultInstructions string

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode != "all" && mode != "poller" && mode != "worker" {
		fmt.Fprintf(os.Stderr, "Usage: aizu [poller|worker]\n\n")
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  (none)   Run both poller and worker (default)\n")
		fmt.Fprintf(os.Stderr, "  poller   Poll GitHub and enqueue tasks to Redis\n")
		fmt.Fprintf(os.Stderr, "  worker   Pull tasks from Redis and run the agent\n")
		os.Exit(1)
	}

	_ = godotenv.Load()

	cfg := config.Load()

	if (mode == "all" || mode == "poller") && len(cfg.Repos) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no repos configured. Set AIZU_REPOS (comma-separated owner/repo).\n")
		os.Exit(1)
	}

	gh := github.New(cfg.GitHubToken, cfg.Signature)
	q := queue.New(cfg.RedisURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Validate the token up front: a rejected token means nothing can work,
	// so fail with instructions instead of letting every poll error out. A
	// network error is not a config problem — warn and keep running.
	if user, err := gh.AuthenticatedUser(ctx); err != nil {
		var se *github.StatusError
		if errors.As(err, &se) {
			fmt.Fprintf(os.Stderr, "Error: GitHub rejected the token (%d). Check GITHUB_TOKEN in .env — it must be a classic personal access token with the `repo` scope (https://github.com/settings/tokens).\n", se.Code)
			os.Exit(1)
		}
		slog.Warn("Could not reach GitHub; continuing", "error", err)
	} else {
		cfg.BotUsername = user.Login
		slog.Info("Authenticated", "login", user.Login, "type", user.Type)
	}

	if mode == "all" || mode == "poller" {
		checkRepos(ctx, gh, cfg)
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("Shutting down…")
		cancel()
	}()

	var wg sync.WaitGroup

	if mode == "all" || mode == "worker" {
		if cfg.AnthropicKey == "" && cfg.OpenAIKey == "" && cfg.OpenAIBaseURL == "" {
			if base := config.AutodetectModelServer(); base != "" {
				cfg.OpenAIBaseURL = base
				slog.Info("Auto-detected local model server", "base_url", base)
			} else {
				slog.Warn("No model credential configured and no local model server found; agent runs will fail. Set OPENAI_BASE_URL, ANTHROPIC_API_KEY, or OPENAI_API_KEY.")
			}
		}
		exec := executor.New(cfg)
		exec.CleanupStale()
		q.RecoverStale(ctx)
		loader := template.NewLoader(defaultInstructions)
		// One shared Worker across goroutines: it is stateless per task, the
		// queue's BRPOP hands each task to exactly one consumer, and the
		// enqueue dedupe keeps at most one active task per issue/PR.
		w := worker.New(q, exec, gh, loader, cfg)
		for i := 0; i < cfg.Concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w.Run(ctx)
			}()
		}
		slog.Info("Worker started", "concurrency", cfg.Concurrency)
	}

	if mode == "all" || mode == "poller" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			poller.New(cfg, gh, q).Run(ctx)
		}()
	}

	slog.Info("Aizu started", "mode", mode)
	<-ctx.Done()
	wg.Wait()
	slog.Info("Shutdown complete")
}

// checkRepos verifies each watched repo is visible to the token, drops the
// ones that aren't (so the poller doesn't spam about them), and exits when
// none are usable. Network errors keep the repo — GitHub being unreachable
// is not a config problem.
func checkRepos(ctx context.Context, gh *github.Client, cfg *config.Config) {
	account := cfg.BotUsername
	if account == "" {
		account = "the token's account"
	}
	var good []string
	for _, repo := range cfg.Repos {
		err := gh.CheckRepo(ctx, repo)
		if err == nil {
			good = append(good, repo)
			continue
		}
		var se *github.StatusError
		if errors.As(err, &se) {
			fmt.Fprintf(os.Stderr, "Error: cannot access %s (%d). Either the name is misspelled, or %s lacks access. For private repos: add the account as a collaborator AND accept the invite from that account.\n", repo, se.Code, account)
			continue
		}
		slog.Warn("Could not verify repo; keeping it", "repo", repo, "error", err)
		good = append(good, repo)
	}
	if len(good) == 0 {
		fmt.Fprintf(os.Stderr, "Error: none of the configured repos are accessible. Fix AIZU_REPOS in .env.\n")
		os.Exit(1)
	}
	cfg.Repos = good
	slog.Info(fmt.Sprintf("Watching %d repo(s) as %s: %s", len(good), account, strings.Join(good, ", ")))
}
