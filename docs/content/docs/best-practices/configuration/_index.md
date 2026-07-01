---
title: "Configuration"
weight: 4
---

# Configuration

## Trigger

```toml
[trigger]
keyword = "aizu"
repos = ["org/repo1", "org/repo2"]
users = ["alice"]      # empty = allow all users
```

- **List repos explicitly** — empty `repos` picks up all repos the token can
  access.
- **Restrict users** in shared orgs to prevent accidental triggers.

## Agent

```toml
[agent]
timeout = 600   # seconds
```

- Default 600s works for most tasks. Reduce to 120–300s for simple fixes to
  fail fast.
- Keep `{prompt_file}` in your `command` string — it is replaced at runtime.

## Poller

```toml
[poller]
interval_seconds = 15
```

- 15s is fine for most setups. Increase to 60s+ with many repos to reduce
  GitHub API usage (5,000 requests/hour limit).

## Queue

- **Use persistent Redis in production.** The default Docker Compose setup is
  ephemeral — tasks are lost on restart. Add a volume:

```yaml
services:
  redis:
    volumes:
      - redis-data:/data
volumes:
  redis-data:
```

## Overrides

All `aizu.toml` settings can be overridden via environment variables (e.g.
`AIZU_TIMEOUT`, `POLL_INTERVAL`, `AIZU_REPOS`). Secrets are environment-only.
