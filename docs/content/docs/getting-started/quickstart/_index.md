---
title: "Quickstart"
weight: 2
---

# Quickstart

Get Aizu running and trigger your first agent.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- A GitHub [personal access token](https://github.com/settings/tokens) with
  `issues: read/write` permissions on the repos you want to watch

## 1. Clone and configure

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
cp .env.example .env
```

Edit `.env` and add your token:

```env
GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE
```

Edit `aizu.toml` to set the repositories Aizu should watch (required):

```toml
[trigger]
repos = ["owner/repo"]
```

You can also adjust the trigger keyword, restrict which users can invoke the
agent, or change the polling interval.

## 2. Start

```bash
docker compose up -d
```

Docker pulls the latest Aizu and agent images from GHCR and starts everything.
Aizu begins polling for `@aizu` mentions immediately.

## 3. Trigger an agent

Comment on any issue or pull request in a watched repository:

```
@aizu fix the failing test in parser_test.go
```

Within one polling interval (30 seconds by default) Aizu reacts with 👀, runs
the agent, and posts the result as a reply. Follow along with:

```bash
docker compose logs -f aizu
```
