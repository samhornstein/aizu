# Aizu

Aizu lets you control your local coding agents directly from GitHub. Mention `aizu` in any issue or pull request and your agent handles it — running on your own machine, with your own models.

```
aizu implement this ticket
aizu review this pull request
```

## Quickstart

Get Aizu running and trigger your first agent.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- A local model server — [Ollama](https://ollama.com/download) is the easiest ([llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start) and [LM Studio](https://lmstudio.ai/) also work), or skip it and use an API key

### 1. Create a GitHub token

Generate a [classic token](https://github.com/settings/tokens) with the `repo` scope on **your own account** (fine-grained tokens can't access repos the account doesn't own). Aizu marks its own replies so it never re-triggers on them, even when it posts as you.

### 2. Download and configure

No clone, no build — one compose file and a two-line `.env`:

```bash
mkdir aizu && cd aizu
curl -fsSLO https://raw.githubusercontent.com/samhornstein/aizu/main/deploy/docker-compose.yml
cat > .env <<EOF
GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE
AIZU_REPOS=owner/repo
EOF
```

See [`.env.example`](.env.example) for all options.

### 3. Start a local model

With [Ollama](https://ollama.com/download) installed, pull a model (Ollama's server runs automatically):

```bash
ollama pull qwen2.5-coder:1.5b
```

That's it — Aizu auto-detects a running Ollama, llama.cpp, or LM Studio server on its standard port at startup. Set `OPENAI_BASE_URL` in `.env` only for a non-standard port or remote host.

<details>
<summary>Using llama.cpp instead</summary>

```bash
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080
```

The `-hf` flag downloads the model automatically (~1 GB on first run).
</details>

> **Using an API key instead?** Set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` in `.env` and skip this step.

### 4. Start

```bash
docker compose up -d
```

Aizu begins polling immediately (the agent sandbox image is pulled on first use). Follow along with:

```bash
docker compose logs -f aizu
```

### 5. Trigger your first agent

Comment on any issue in a watched repo:

```
aizu hello
```

Within one polling interval (15 seconds by default) Aizu reacts with 👀, runs the agent, and posts the result as a reply.

> **Who can trigger?** By default only users with write access to the repo — safe even on public repos. `AIZU_USERS` allowlists specific logins instead; `AIZU_ALLOW_ALL=true` disables the gate (never on a public repo).

> **Note:** With a small local model the reply may be incoherent or raw JSON — that's expected. The goal here is just to confirm the pipeline works end-to-end. For real tasks, use a larger model or an API key.

## Give Aizu its own identity (optional)

By default Aizu posts replies as whoever owns the token. If you'd rather have replies attributed to a separate account, create one at [github.com/join](https://github.com/join) using `yourname+aizu@gmail.com` as the email and `yourname-aizu` as the username — GitHub treats the `+` address as separate but it lands in your existing inbox. Generate the classic token from that account instead.

> **Private repos:** Add the bot account as a collaborator first: **Settings → Collaborators → Add people**. Then log in as the bot account and accept the collaboration invite — the token won't have access until the invite is accepted.

## Building from source

Contributors (or anyone who wants to run exactly what's in the tree):

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
docker compose build agent   # local agent sandbox image
docker compose up -d --build
```

The root `docker-compose.yml` builds from source and uses the locally built agent image; the published images are only used by `deploy/docker-compose.yml`.

## Docs

Full documentation: `make docs-serve`
