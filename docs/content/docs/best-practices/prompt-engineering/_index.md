---
title: "Prompt Engineering"
weight: 3
---

# Prompt Engineering

The agent receives your comment as-is, so be specific and actionable.

## Good prompts

```
@aizu fix the null pointer in parseConfig() in config/parser.go
```

```
@aizu add input validation to the signup handler.
Reject emails without an @ symbol and passwords shorter than 8 characters.
Add tests for both cases.
```

## Bad prompts

```
@aizu fix the bugs        # too vague — which file? what bugs?
```

```
@aizu refactor the codebase to use error wrapping   # too broad — will timeout
```

## Tips

1. **Mention file paths.** The agent needs direction.
2. **Describe expected behavior.** "Should return an error" > "is broken".
3. **One task per comment.** Batch requests may be partially fulfilled.
4. **Ask for tests.** Add "and add tests" to ensure coverage.

## PRs vs issues

- **PR comments** — agent pushes commits directly to the PR branch
- **Issue comments** — agent creates a new branch and opens a PR

## Iterating

Follow up in the same thread if the first response needs adjustment. The agent
sees the full conversation history.
