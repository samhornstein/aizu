---
title: "Security"
weight: 1
---

# Security

## Token management

Aizu supports two authentication methods:

### GitHub App (recommended)

GitHub Apps provide better security than PATs:

- **Per-repo installation** — grant access only to specific repos
- **Scoped permissions** — request only the permissions you need
- **Automatic token rotation** — Aizu refreshes installation tokens every hour
  automatically
- **No long-lived secrets** — installation tokens expire after 1 hour

See [Quickstart](../getting-started/quickstart/) for setup instructions.

### Personal Access Token (PAT)

- **Use a dedicated bot account.** Never run Aizu with your personal GitHub
  token.
- **Use a classic PAT with only the `repo` scope.** Avoid broader scopes like
  `admin:org` or `write:packages`.
- **Rotate tokens regularly.** Aizu reads `GITHUB_TOKEN` at startup, so
  rotation is a simple `docker compose restart`.
- **Never commit `.env` to git.**

## Restrict triggers

By default any comment with the trigger keyword fires the agent. For public
repos or shared orgs, restrict to trusted users:

```toml
[trigger]
users = ["alice", "bob"]
```

## Container isolation

Each task runs in a fresh container with `--memory=4g` and `--cpus=2` limits.
Containers are destroyed after each task.

- **Lower limits for small repos** — `--memory=2g` and `--cpus=1` are often
  sufficient.
- **Network isolation** — if using a local model, bind it to `127.0.0.1` or
  use Docker network isolation so it is not publicly exposed.
