---
title: "Development"
weight: 2
bookFlatSection: true
bookToc: false
---

# Development

## Prerequisites

- [Go](https://golang.org/dl/) (see `go.mod` for the required version)
- [Docker](https://docs.docker.com/get-docker/)
- [golangci-lint](https://golangci-lint.run/welcome/install/) — required by the pre-commit hook

## Setup

```bash
git clone https://github.com/samhornstein/aizu.git && cd aizu
make install-hooks   # wire pre-commit and commit-msg hooks (once after cloning)
```

## Common tasks

```bash
make build      # compile ./bin/aizu
make test       # run tests
make vet fmt    # static analysis and formatting
```

## Running locally

```bash
cp .env.example .env   # fill in GITHUB_TOKEN and model credentials
mkdir -p .aizu && cp aizu.toml.example .aizu/config.toml
```

Edit `.aizu/config.toml` to set the repos to watch:

```toml
[trigger]
repos = ["owner/repo"]
```

```bash
make up                        # start Aizu + Redis via Docker Compose
docker compose logs -f aizu    # tail logs
```

Or run the two halves separately (useful during development):

```bash
make build
./bin/aizu poller   # poll GitHub and enqueue tasks
./bin/aizu worker   # dequeue tasks and run the agent
```

To build the Aizu image locally instead of pulling from GHCR:

```bash
docker compose up -d --build
```

## Commits

This project follows [Conventional Commits](https://www.conventionalcommits.org/).
The `commit-msg` hook installed by `make install-hooks` enforces this at commit
time. The format is:

```
<type>[(<scope>)]: <description>

Types: feat  fix  docs  refactor  test  chore  ci
```

## Docs

```bash
make docs-serve   # live-reload Hugo site at http://localhost:1313
```
