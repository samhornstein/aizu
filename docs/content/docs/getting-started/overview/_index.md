---
title: "Overview"
weight: 1
---

# Overview

Aizu is a self-hosted service that connects your GitHub repositories to a coding
agent. When someone mentions the trigger keyword (`@aizu` by default) in an
issue or pull-request comment, Aizu runs an agent in an isolated sandbox to act
on the request, then replies on the thread.

```
@aizu fix the failing test in parser_test.go
@aizu add input validation to the signup handler
@aizu why is this function allocating on every call?
```

## How it works

Aizu has two cooperating parts, coordinated through a Redis queue:

1. **Poller** — on a fixed interval, asks GitHub for comments created since the
   last check (`GET /repos/{owner}/{repo}/issues/comments?since=…`). This single
   endpoint covers comments on both issues and pull requests. Any comment
   containing the trigger keyword becomes a task on the queue.
2. **Worker** — pulls a task, reacts to the comment with 👀, clones the
   repository into a fresh Docker container, runs the agent against the
   conversation, and posts the result back as a comment. On a pull request the
   agent works on the PR's branch; on an issue it opens a new pull request.

```
GitHub  ──poll──▶  Poller  ──enqueue──▶  Redis  ──dequeue──▶  Worker  ──run──▶  Agent (Docker)
   ▲                                                                                  │
   └──────────────────────────── reply / commit / open PR ◀───────────────────────────┘
```

The queue sits between polling and execution so that the moving parts stay
decoupled and tasks are never lost or run twice.

## Scope

This version is deliberately focused. It:

- Polls **issue and pull-request comments** via the GitHub REST API.
- Authenticates with a **personal access token (PAT)**.
- Runs a **single agent**.

Designed-for-later (not yet supported): comments in **Discussions** (which
require the GraphQL API), **GitHub App** authentication, **webhooks**, other
clients such as **Linear**, and **multiple concurrent agents** — the last of
which the Redis queue already makes a small step.

Next: [Quickstart](../quickstart/).
