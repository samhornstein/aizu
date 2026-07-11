---
title: "Quickstart"
weight: 2
---

# Quickstart

Get Aizu running and trigger your first agent.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- A local model server — [Ollama](https://ollama.com/download) is the easiest ([llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start) and [LM Studio](https://lmstudio.ai/) also work), or skip it and use an API key

## 1. Create a GitHub token

Generate a [classic token](https://github.com/settings/tokens) with the `repo` scope. A fine-grained token also works for repos your account owns (Contents, Issues, and Pull requests read/write) — but not for repos you only collaborate on, so classic is the simpler default. Aizu marks its own replies so it never re-triggers on them, even when it posts as you.

> **Want replies from a separate identity?** Create a dedicated account at [github.com/join](https://github.com/join) using `yourname+aizu@gmail.com` as the email and `yourname-aizu` as the username — GitHub treats the `+` address as separate but it lands in your existing inbox. Generate the classic token from that account instead (fine-grained won't work here — the bot account doesn't own your repos). For private repos, add the bot account as a collaborator (**Settings → Collaborators → Add people**) and accept the invite from the bot account before the token will work.

## 2. Download and configure

No clone, no build — one compose file and a two-line `.env`:

```bash
mkdir aizu && cd aizu
curl -fsSLO https://raw.githubusercontent.com/samhornstein/aizu/main/deploy/docker-compose.yml
cat > .env <<EOF
GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE
AIZU_REPOS=owner/repo
EOF
```

See the repo's `.env.example` for all options. To build from source instead, see [Development](../../development/).

## 3. Start a local model

With [Ollama](https://ollama.com/download) installed, pull a model (Ollama's server runs automatically):

```bash
ollama pull qwen2.5-coder:1.5b
```

That's it — Aizu auto-detects a running Ollama, llama.cpp, or LM Studio server on its standard port at startup. Set `OPENAI_BASE_URL` in `.env` only for a non-standard port or remote host.

Using llama.cpp instead:

```bash
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080
```

The `-hf` flag downloads the model automatically (~1 GB on first run).

> **Using an API key instead?** Set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` in `.env` and skip this step.

## 4. Start

```bash
docker compose up -d
```

Aizu begins polling for `aizu` mentions immediately (the agent sandbox image is pulled on first use). Follow along with:

```bash
docker compose logs -f aizu
```

## 5. Trigger your first agent

Comment on any issue in a watched repo:

```
aizu hello
```

Within one polling interval (15 seconds by default) Aizu reacts with 👀, runs the agent, and posts the result as a reply.

> **Note:** With a small local model the reply may be incoherent or raw JSON — that's expected. The goal here is just to confirm the pipeline works end-to-end. For real tasks, use a larger model or an API key.

## Troubleshooting

**Follow live logs:**
```bash
docker compose logs -f aizu
```

**Update to the latest release:**
```bash
docker compose pull && docker compose up -d
```

**Restart** (e.g. after editing `.env`):
```bash
docker compose restart aizu
```

**Aizu isn't picking up comments:**
- Check that your message begins with the trigger keyword (`AIZU_TRIGGER`, default `aizu`).
- Check that `AIZU_REPOS` matches the repo exactly (`owner/repo`).
- The poller runs every 15 seconds; wait one interval then check the logs.

**Agent fails with "no models returned" or connection error:**
- Make sure your model server is still running (`curl http://localhost:11434/v1/models` for Ollama, port 8080 for llama.cpp, 1234 for LM Studio).
- Auto-detection happens once at startup — if you started the model server afterwards, restart Aizu (`docker compose restart aizu`).

**Container exits immediately:**
- Run `docker compose logs aizu` (without `-f`) to see the error before it restarts.
- `no repos configured` means `AIZU_REPOS` is empty — set it to a comma-separated `owner/repo` list.
