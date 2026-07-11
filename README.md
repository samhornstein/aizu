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
- [llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start) — `brew install llama.cpp` on Mac

### 1. Create a GitHub token

Generate a [classic token](https://github.com/settings/tokens) with the `repo` scope on **your own account** (fine-grained tokens can't access repos the account doesn't own). Aizu marks its own replies so it never re-triggers on them, even when it posts as you.

### 2. Clone and configure

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
```

Create a `.env` with your token and the repositories to watch (see
`.env.example` for all options):

```env
GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE
AIZU_REPOS=owner/repo
```

### 3. Start a local model

Run a model server. The `-hf` flag downloads the model automatically (~1 GB on first run):

```bash
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080
```

Then in `.env`, uncomment:

```env
OPENAI_BASE_URL=http://host.docker.internal:8080/v1
```

> **Using an API key instead?** Set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` in `.env` and skip this step.

### 4. Start

Build the agent sandbox image (first run only), then start Aizu:

```bash
docker compose build agent
docker compose up -d
```

Aizu begins polling immediately. Follow along with:

```bash
docker compose logs -f aizu
```

### 5. Trigger your first agent

Comment on any issue in a watched repo:

```
aizu hello
```

Within one polling interval (15 seconds by default) Aizu reacts with 👀, runs the agent, and posts the result as a reply.

> **Note:** With a small local model the reply may be incoherent or raw JSON — that's expected. The goal here is just to confirm the pipeline works end-to-end. For real tasks, use a larger model or an API key.

## Give Aizu its own identity (optional)

By default Aizu posts replies as whoever owns the token. If you'd rather have replies attributed to a separate account, create one at [github.com/join](https://github.com/join) using `yourname+aizu@gmail.com` as the email and `yourname-aizu` as the username — GitHub treats the `+` address as separate but it lands in your existing inbox. Generate the classic token from that account instead.

> **Private repos:** Add the bot account as a collaborator first: **Settings → Collaborators → Add people**. Then log in as the bot account and accept the collaboration invite — the token won't have access until the invite is accepted.

## Docs

Full documentation: `make docs-serve`
