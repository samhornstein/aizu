# Aizu

Aizu lets you control your local coding agents directly from GitHub. Mention `@aizu` in any issue or pull request and your agent handles it — running on your own machine, with your own models.

```
@aizu fix the failing test in parser_test.go
@aizu add input validation to the signup handler
```

## Quickstart

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
cp .env.example .env
```

Add your `GITHUB_TOKEN` to `.env` and set your repos in `aizu.toml`:

```toml
[trigger]
repos = ["owner/repo"]
```

Start a local model server (downloads ~1 GB on first run; requires [llama.cpp](https://github.com/ggml-org/llama.cpp#quick-start)):

```bash
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080
```

Uncomment `MODEL_SERVER_HOST=host.docker.internal:8080` in `.env`, then:

```bash
docker compose up -d
```

Comment `@aizu hello` on an issue in a watched repo — you should see a 👀 reaction and a reply within 30 seconds.

## Docs

Full documentation: `make docs-serve`
