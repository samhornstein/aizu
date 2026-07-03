---
title: "Production Deployment"
weight: 5
---

# Production Deployment

## Monitoring

Watch for these log events:

- `WARN Engine timed out` — increase `AIZU_TIMEOUT` or check model health
- `ERROR docker run` — Docker daemon issue or resource exhaustion
- `ERROR git clone` — token expired or repo access revoked

Configure log rotation in `docker-compose.yml`:

```yaml
services:
  aizu:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

## Resources

- **Memory:** At least 8 GB (4 GB per agent container + overhead)
- **CPU:** At least 4 cores
- **Disk:** 10 GB+ for Docker images and cloned repos
- **Redis persistence** — add a volume to Redis so tasks survive restarts

## Scaling

A single instance handles dozens of repos. The bottleneck is usually the model
server, not Aizu. To scale:

- Increase poll interval to reduce GitHub API usage
- Use a faster model to reduce task duration
- Run multiple Aizu instances against the same Redis queue (partition repos
  with `AIZU_REPOS`)

## Updates

```bash
git pull origin main
docker compose up -d --build
```

## Backup

Back up `.env` (tokens), `.aizu/config.toml` (config), and Redis data if using
persistence.
