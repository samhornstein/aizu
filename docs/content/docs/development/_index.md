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
cp .env.example .env   # set GITHUB_TOKEN, AIZU_REPOS, and model credentials
```

```bash
make up                        # build the agent image, then start Aizu + Redis
docker compose logs -f aizu    # tail logs
```

Or run the two halves separately (useful during development):

```bash
make build
./bin/aizu poller   # poll GitHub and enqueue tasks
./bin/aizu worker   # dequeue tasks and run the agent
```

`docker compose up` (via `make up`) builds Aizu from your working tree — the
published ghcr.io images are only used by the no-clone install
(`deploy/docker-compose.yml`). After changing code or config, rebuild with:

```bash
docker compose up -d --build
```

The agent sandbox image (`aizu-agent:pi`, from `templates/pi/Dockerfile`) is
also built from source, via the `agent` Compose profile — `make up` builds it
for you. The tag names the engine; to run a different agent, add
`templates/<engine>/Dockerfile` and set `CONTAINER_IMAGE`/`ENGINE_COMMAND` for
it. Rebuild after changing the agent Dockerfile:

```bash
docker compose build agent
```

## Commits

This project follows [Conventional Commits](https://www.conventionalcommits.org/).
The format is:

```
<type>[(<scope>)][!]: <description>

Types: feat  fix  docs  style  refactor  perf  test  build  ci  chore  revert
```

Enforcement happens in two places: the `commit-msg` hook installed by
`make install-hooks` checks messages at commit time, and the `PR Title` workflow
checks the pull-request title in CI. Because PRs are squash-merged, the PR title
becomes the commit on `main` — and the input to versioning below.

## Releasing

Releases are intentional, and the version is computed automatically from the
commits since the last tag: git-cliff maps `feat` → minor, `fix` → patch, and
`!` / `BREAKING CHANGE` → major.

To cut one, go to **Actions → Release → Run workflow** and leave *version* blank
— the workflow computes the next version, tags the commit, generates the
changelog, and publishes a GitHub Release. Provide *version* explicitly (e.g.
`v1.2.0`) only to override the computed value.

Each release also publishes multi-arch images to ghcr.io
(`ghcr.io/samhornstein/aizu` and `ghcr.io/samhornstein/aizu-agent-pi`, tagged
`latest` and the version) — these are what the no-clone install pulls. Source
builds are unaffected; `git describe --tags` in a clone shows what a
source-built instance runs.

## Docs

```bash
make docs-serve   # live-reload Hugo site at http://localhost:1313
```
