---
title: "Security"
weight: 1
---

# Security

## Token management

- **Consider a dedicated bot account.** A personal token works, but a
  separate account limits the blast radius if the token leaks and keeps
  agent activity attributed to a distinct identity — recommended for
  anything beyond personal projects.
- **Use a classic PAT with only the `repo` scope.** Avoid broader scopes like
  `admin:org` or `write:packages`.
- **Rotate tokens regularly.** Aizu reads `GITHUB_TOKEN` at startup, so
  rotation is a simple `docker compose restart`.
- **Never commit `.env` to git.**

## Restrict triggers

A trigger runs an agent on **your** machine, so who may trigger matters. By
default only users with **write or admin permission** on the repo can trigger;
everyone else is silently ignored (one log line explains the deny).

Two overrides in `.env`:

```env
# Exactly these logins may trigger — replaces the write-access check
# (useful to admit a trusted outside contributor):
AIZU_USERS=alice,bob

# DANGER: let anyone who can comment trigger the agent. Never use this
# on a public repo — any GitHub account could run code on your machine
# and spend your tokens/compute.
AIZU_ALLOW_ALL=true
```

Permission lookups are cached for 10 minutes, so revoking someone's access
takes effect within that window.

## Container isolation

Each task runs in a fresh container with `--memory=4g` and `--cpus=2` limits.
Containers are destroyed after each task.

- **Lower limits for small repos** — `--memory=2g` and `--cpus=1` are often
  sufficient.
- **Network isolation** — if using a local model, bind it to `127.0.0.1` or
  use Docker network isolation so it is not publicly exposed.
