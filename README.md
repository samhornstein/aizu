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

```bash
docker compose up -d
```

Then comment `@aizu …` on an issue or PR in a watched repo.

## Docs

Full documentation: `make docs-serve`
