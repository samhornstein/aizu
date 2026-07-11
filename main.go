package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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

	gh := github.New(cfg.GitHubToken)
	q := queue.New(cfg.RedisURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve the token's login for logging. Self-comment filtering does not
	// depend on it: Aizu's replies carry a content marker the poller skips.
	if user, err := gh.AuthenticatedUser(ctx); err != nil {
		slog.Warn("Could not resolve authenticated user; check GITHUB_TOKEN", "error", err)
	} else {
		cfg.BotUsername = user.Login
		slog.Info("Authenticated", "login", user.Login, "type", user.Type)
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("Worker started")
			worker.New(q, exec, gh, loader).Run(ctx)
		}()
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
