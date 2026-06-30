---
title: "Security"
weight: 1
---

# Security

Aizu runs code in isolated Docker containers, but you should still follow these
practices to keep your repositories and credentials safe.

## Token management

- **Use a dedicated bot account.** Never run Aizu with your personal GitHub
  token. A bot account limits blast radius if the token is compromised.
- **Use a classic PAT with minimal scopes.** The `repo` scope is required;
  avoid adding `admin:org` or `write:packages`.
- **Rotate tokens regularly.** Since Aizu reads `GITHUB_TOKEN` from the
  environment at startup, rotation is a simple `docker compose restart`.
- **Never commit `.env` to git.** The `.gitignore` already excludes it, but
  double-check before pushing.

## Restrict who can trigger the agent

By default, any comment containing the trigger keyword fires the agent.
Restrict this to trusted users:

```toml
[trigger]
users = ["alice", "bob"]
```

This is especially important for:

- **Public repositories** — anyone can comment and trigger code changes
- **Organizations** — prevent junior devs from accidentally triggering expensive runs

## Container isolation

Aizu runs each agent task in a fresh Docker container with these defaults:

- `--memory=4g` — hard memory limit per task
- `--cpus=2` — hard CPU limit per task
- Containers are destroyed after each task via `CleanupStale`

**Recommendations:**

- **Lower resource limits for small repos.** If your repo is under 10 MB,
  `--memory=2g` and `--cpus=1` are usually sufficient. Adjust via the agent
  image's Dockerfile or by forking it.
- **Run Aizu as a non-root user** on the host. The Docker daemon requires
  root access, but the Aizu process itself does not.
- **Network isolation.** The agent containers can reach your model server and
  GitHub. If you use a local model, ensure it's not exposed to the public
  network (bind to `127.0.0.1` or use Docker network isolation).

## Secrets in the agent container

Aizu passes `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and `OPENAI_BASE_URL` into
the agent container as environment variables. These are:

- **Not persisted** — containers are ephemeral
- **Not logged** — Aizu redacts secrets in its output
- **Accessible only within the container** — they are not written to disk

If you use additional secrets, pass them through environment variables in
`.env` and add them to the agent container's environment in `docker-compose.yml`.

## Private repositories

To give Aizu access to private repos:

1. Add the bot account as a collaborator on the repo
   (**Settings → Collaborators → Add people**)
2. Ensure the PAT has the `repo` scope (classic) or `Contents: Read` scope
   (fine-grained)

For organization repos, add the bot as an outside collaborator or member of the
appropriate team.
