---
title: "Configuration"
weight: 4
---

# Configuration

Aizu is configured entirely through environment variables, set in `.env`. The
[`.env.example`](https://github.com/samhornstein/aizu/blob/main/.env.example)
file is the complete reference — every setting, its format, and its default.
Only `GITHUB_TOKEN`, `AIZU_REPOS`, and one model credential are required.

This page covers the settings that involve a trade-off worth understanding.

## Trigger

- **`AIZU_REPOS`** — the repos to watch (`owner/repo`, comma-separated). Left
  empty, Aizu picks up every repo the token can access, which is rarely what you
  want; list them explicitly.
- **`AIZU_USERS`** — in shared orgs, restrict who can trigger Aizu to avoid
  accidental or unwanted runs. Empty allows everyone.
- **`AIZU_TRIGGER`** — the keyword must appear at the **start** of the comment or
  issue body, so an incidental mention elsewhere in the text won't fire a run.

## Agent

- **`AIZU_TIMEOUT`** — defaults to a generous 1-hour limit for long agent runs;
  lower it (e.g. 300–600s) to fail fast on simple tasks.
- **`AIZU_ENGINE`** — picks a coding-agent preset (`pi` default, `claude` for
  Claude Code); it sets the sandbox image and command for you.
- **`ENGINE_COMMAND`** — overrides the preset's command, for custom agents.
  Must keep the `{prompt_file}` placeholder, which is replaced with the
  prompt's path at runtime (`{model}` is replaced with the discovered local
  model ID, if present).
- **`CONTAINER_IMAGE`** — overrides the preset's sandbox image. Swap it
  (together with `ENGINE_COMMAND`) to run an agent without a preset.

## Poller

- **`POLL_INTERVAL`** — 15s suits most setups. Increase to 60s+ when watching
  many repos to stay well under GitHub's API rate limit (5,000 requests/hour).

## Queue

- **Redis is persistent by default** — the Compose setup mounts a `redis-data`
  volume, so queued tasks survive restarts. Set **`REDIS_URL`** to point at an
  external or hosted Redis.

## Secrets

`GITHUB_TOKEN` and model credentials are environment-only — keep them in `.env`
(which is gitignored), never committed to the repo.
