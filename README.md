# Aizu

Aizu lets you control your local coding agents directly from GitHub. Mention `@aizu` in any issue or pull request and your agent handles it — running on your own machine, with your own models.

```
@aizu fix the failing test in parser_test.go
@aizu add input validation to the signup handler
```

## Quickstart

Get Aizu running and trigger your first agent.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start) — `brew install llama.cpp` on Mac

### 1. Set up GitHub authentication

Choose one of the two authentication methods:

**Option A: GitHub App (recommended)** — per-repo installation, scoped permissions, automatic token rotation.

1. Create a GitHub App: **Settings → Developer settings → GitHub Apps → New GitHub App**
2. Set permissions: Issues (R/W), Pull requests (R/W), Contents (Read-only)
3. Generate and download a private key (`.pem`)
4. Install the app on your account/org
5. Find the installation ID at `https://github.com/settings/installations`
6. Set in `.env`: `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_KEY`

**Option B: Personal Access Token (PAT)** — use a dedicated bot account with a classic `repo`-scoped token.

### 2. Clone and configure

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
cp .env.example .env
```

Edit `.env` with your chosen authentication method.

Edit `aizu.toml` to set the repositories Aizu should watch (required):

```toml
[trigger]
repos = ["owner/repo"]
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

```bash
docker compose up -d
```

Aizu begins polling for `@aizu` mentions immediately. Follow along with:

```bash
docker compose logs -f aizu
```

### 5. Trigger your first agent

From your **personal** account, comment on any issue in a watched repo:

```
@aizu hello
```

Within one polling interval (15 seconds by default) Aizu reacts with 👀, runs the agent, and posts the result as a reply.

> **Note:** With a small local model the reply may be incoherent or raw JSON — that's expected. The goal here is just to confirm the pipeline works end-to-end. For real tasks, use a larger model or an API key.

## Docs

Full documentation: `make docs-serve`
