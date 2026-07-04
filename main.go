// Command aizu is a self-hosted GitHub agent. It polls a repository's issue and
// pull-request comments for a trigger keyword (default "@aizu") and runs an
// isolated coding agent for each mention, posting the result back as a comment.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/samhornstein/aizu/internal/webhook"
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
		fmt.Fprintf(os.Stderr, "Error: no repos configured. Set [trigger].repos in aizu.toml or AIZU_REPOS env var.\n")
		os.Exit(1)
	}

	gh := github.New(cfg.GitHubToken)
	q := queue.New(cfg.RedisURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve the token's own login so we never react to our own comments.
	if user, err := gh.AuthenticatedUser(ctx); err != nil {
		slog.Warn("Could not resolve authenticated user; self-comment filtering disabled", "error", err)
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
		exec := executor.New(cfg)
		exec.CleanupStale()
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

	// Start webhook HTTP server if enabled.
	if cfg.WebhookEnabled {
		handler := webhook.New(cfg, gh, q)
		mux := http.NewServeMux()
		mux.HandleFunc("/", handler.ServeHTTP)
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		})
		server := &http.Server{Addr: ":8080", Handler: mux}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("Webhook server started", "addr", ":8080")
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("Webhook server error", "error", err)
			}
		}()
		go func() {
			<-ctx.Done()
			_ = server.Shutdown(context.Background())
		}()
	}

	slog.Info("Aizu started", "mode", mode, "webhook", cfg.WebhookEnabled)
	<-ctx.Done()
	wg.Wait()
	slog.Info("Shutdown complete")
}
