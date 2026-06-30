---
title: "Configuration"
weight: 4
---

# Configuration

Tuning `aizu.toml` and environment variables for your workload.

## Trigger configuration

```toml
[trigger]
keyword = "aizu"       # no @ prefix — Aizu adds it automatically
repos = ["org/repo1", "org/repo2"]
users = ["alice"]      # empty = allow all users
```

**Tips:**

- **Use a unique keyword.** If your team uses `@bot` or `@assistant` for
  other tools, pick something unique like `@aizu` or `@code-agent`.
- **List repos explicitly.** Avoid relying on auto-discovery (empty `repos`
  list) in production — it picks up all repos the token can access.
- **Restrict users in shared orgs.** This prevents accidental triggers from
  CI bots or inactive members.

## Agent configuration

```toml
[agent]
image = "ghcr.io/samhornstein/aizu-agent:pi"
command = "pi -p \"$(cat {prompt_file})\""
timeout = 600   # seconds
```

**Tips:**

- **Timeout tuning.** The default 600s (10 min) works for most tasks. Reduce
  to 120–300s for simple fixes to fail fast on unresponsive models.
- **Custom agent images.** Fork the agent image if you need additional tools
  (e.g., a linter, formatter, or language-specific tooling).
- **The `{prompt_file}` placeholder** is replaced with the path to the prompt
  file at runtime. Keep it in your command string.

## Poller configuration

```toml
[poller]
interval_seconds = 15
```

**Tips:**

- **Default 15s is usually fine.** Lower values increase GitHub API usage
  without much practical benefit.
- **Increase to 60s+** if you have many repos and want to reduce API calls.
- **GitHub rate limits:** The REST API allows 5,000 requests/hour for
  authenticated users. With 15s polling across 10 repos, that's ~2,400
  requests/hour — safe but worth monitoring.

## Queue configuration

```toml
[queue]
redis_url = "redis://localhost:6379"
```

**Tips:**

- **Use a persistent Redis** in production. The default Docker Compose setup
  uses an ephemeral Redis — tasks are lost on restart.
- **For self-hosted Redis**, use a managed provider or add a volume:

```yaml
# docker-compose.yml
services:
  redis:
    volumes:
      - redis-data:/data
volumes:
  redis-data:
```

## Environment variable overrides

All `aizu.toml` settings can be overridden via environment variables:

| TOML key | Environment variable |
|----------|---------------------|
| `queue.redis_url` | `REDIS_URL` |
| `trigger.keyword` | `AIZU_TRIGGER` |
| `trigger.repos` | `AIZU_REPOS` (comma-separated) |
| `trigger.users` | `AIZU_USERS` (comma-separated) |
| `agent.image` | `CONTAINER_IMAGE` |
| `agent.command` | `ENGINE_COMMAND` |
| `agent.timeout` | `AIZU_TIMEOUT` |
| `poller.interval_seconds` | `POLL_INTERVAL` |

This is useful for CI/CD pipelines or multi-environment deployments where you
don't want to maintain multiple `aizu.toml` files.

## Configuration precedence

Settings are resolved in this order (later overrides earlier):

1. Built-in defaults
2. `aizu.toml` file (path overridable via `AIZU_CONFIG`)
3. Environment variables

Secrets (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`)
are **environment-only** and cannot be set in `aizu.toml`.
