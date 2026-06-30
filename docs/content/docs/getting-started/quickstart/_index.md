---
title: "Quickstart"
weight: 2
---

# Quickstart

Get Aizu running and trigger your first agent.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start) — `brew install llama.cpp` on Mac

## 1. Set up a GitHub bot account

Aizu needs its own GitHub identity so it can post replies without conflicting with your personal account.

Create a new account at [github.com/join](https://github.com/join) using `yourname+aizu@gmail.com` as the email and `yourname-aizu` as the username — GitHub treats the `+` address as separate but it lands in your existing inbox. GitHub also supports multiple logged-in accounts, so you can switch between them without signing out.

Then from the bot account, generate a [classic token](https://github.com/settings/tokens) with the `repo` scope (fine-grained tokens can't access repos the account doesn't own).

> **Private repos:** Add the bot account as a collaborator first: **Settings → Collaborators → Add people**.

## 2. Clone and configure

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
cp .env.example .env
```

Edit `.env` and add the bot account's token:

```env
GITHUB_TOKEN=ghp_YOUR_BOT_TOKEN_HERE
```

Edit `aizu.toml` to set the repositories Aizu should watch (required):

```toml
[trigger]
repos = ["owner/repo"]
```

## 3. Start a local model

Run a model server. The `-hf` flag downloads the model automatically (~1 GB on first run):

```bash
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080
```

Then in `.env`, uncomment:

```env
OPENAI_BASE_URL=http://host.docker.internal:8080/v1
```

> **Using an API key instead?** Set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` in `.env` and skip this step.

## 4. Start

```bash
docker compose up -d
```

Aizu begins polling for `@aizu` mentions immediately. Follow along with:

```bash
docker compose logs -f aizu
```

## 5. Trigger your first agent

From your **personal** account, comment on any issue in a watched repo:

```
@aizu hello
```

Within one polling interval (30 seconds by default) Aizu reacts with 👀, runs the agent, and posts the result as a reply.
