---
title: "Security"
weight: 1
---

# Security

## Threat model

An agent run executes model-chosen commands in a sandbox container on your
machine. The container is isolated from your filesystem, but it holds real
secrets — `GITHUB_TOKEN`, any model API keys, and the repo contents — and it
has **full network egress**. A misbehaving or prompt-injected agent (e.g. one
that read hostile instructions embedded in a pull request diff) could send
any of that anywhere.

There is deliberately no egress allowlist: real tasks need package registries
(`npm install`, `pip install`, `go mod download`), and a registry anyone can
publish to is itself an exfiltration channel — so an allowlist either breaks
dependency installs or doesn't stop a determined exfiltrator (the full
analysis is in [#76](https://github.com/samhornstein/aizu/issues/76)).
Instead, limit what a leaked secret is *worth*, in this order:

1. **Scope the token** — a dedicated bot account with access to only the
   watched repos caps the blast radius no matter where the token travels
   (see below).
2. **Control who can trigger** — the write-access gate is the default; keep
   it (see [Restrict triggers](#restrict-triggers)).
3. **Bound each run** — `AIZU_MAX_RUNS_PER_HOUR` and `AIZU_TIMEOUT` limit
   how much a bad run can do.
4. **Redaction** — configured secrets are redacted from anything Aizu posts
   back to GitHub, which stops accidental leaks (not deliberate
   exfiltration).

## Token management

- **Use a dedicated bot account for public repos.** A personal token carries
  `repo` scope over *every* repo you own; a bot account added as a
  collaborator on only the watched repos limits the blast radius if the
  token leaks, and keeps agent activity attributed to a distinct identity.
  For private personal projects a personal token is fine.
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
