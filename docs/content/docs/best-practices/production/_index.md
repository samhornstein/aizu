---
title: "Production Deployment"
weight: 5
---

# Production Deployment

Running Aizu reliably in production requires attention to monitoring, resource
management, and maintenance.

## Monitoring

### Logs

Aizu logs structured output to stdout. In Docker Compose:

```bash
# Follow live logs
docker compose logs -f aizu

# Search for errors
docker compose logs aizu 2>&1 | grep -i error

# Recent logs only
docker compose logs --tail=100 aizu
```

**Key log events to watch for:**

- `WARN Engine timed out` — increase `AIZU_TIMEOUT` or check model health
- `ERROR docker run` — Docker daemon issue or resource exhaustion
- `ERROR git clone` — token expired or repo access revoked

### Structured logging

Aizu uses Go's `log/slog` package. In production, consider piping logs to a
structured logging backend:

```yaml
# docker-compose.yml
services:
  aizu:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

## Resource management

### Docker cleanup

Aizu automatically cleans up stale containers (labeled `aizu=true`) at startup.
However, you should also:

```bash
# Periodically prune unused Docker resources
docker system prune -f --filter "label=aizu=true"

# Remove dangling images
docker image prune -f
```

### Host resource limits

Ensure your host has enough resources for concurrent agent runs:

- **Memory:** At least 8 GB (4 GB per agent container + overhead)
- **CPU:** At least 4 cores (2 per agent container)
- **Disk:** 10 GB+ for Docker images and cloned repos

### Redis persistence

For production, configure Redis to persist its data:

```yaml
# docker-compose.yml
services:
  redis:
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
volumes:
  redis-data:
```

This ensures in-progress tasks survive a Redis restart.

## Scaling

### Multiple repos

A single Aizu instance can monitor dozens of repos. The bottleneck is usually
the model server, not Aizu itself. If you hit limits:

- **Increase poll interval** to reduce GitHub API usage
- **Use a faster model** to reduce task duration
- **Add resource limits** in `docker-compose.yml` to prevent one task from
  starving others

### Multiple Aizu instances

Running multiple Aizu instances against the same Redis queue is supported —
tasks are consumed by whichever worker picks them up first. However:

- Each instance needs its own `GITHUB_TOKEN` (or share one with caution)
- Ensure only one instance pushes to a given PR branch to avoid conflicts
- Use `AIZU_REPOS` to partition repos across instances if needed

## Updates

### Aizu updates

```bash
# Pull latest changes
git pull origin main

# Rebuild and restart
docker compose up -d --build
```

### Agent image updates

The default agent image (`ghcr.io/samhornstein/aizu-agent:pi`) is built from
the `templates/pi/Dockerfile` in this repo. To update:

```bash
docker compose pull aizu-agent  # if using a separate agent service
# or
docker pull ghcr.io/samhornstein/aizu-agent:pi
```

### Model updates

For local models, update by re-running `llama-server` with the desired model.
For cloud models, update your API key or model selection in `.env`.

## Backup and recovery

### What to back up

- `.env` file (contains your tokens)
- `aizu.toml` configuration
- Redis data (if using persistence)

### Recovery steps

1. Restore `.env` and `aizu.toml`
2. Start Redis with `docker compose up -d redis`
3. Start Aizu with `docker compose up -d aizu`
4. Any queued tasks will be processed; completed tasks are not recoverable
