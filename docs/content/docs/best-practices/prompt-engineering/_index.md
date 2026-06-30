---
title: "Prompt Engineering"
weight: 3
---

# Prompt Engineering

How you phrase your `@aizu` request significantly affects the quality of the
result. The agent receives your comment as-is, so be specific and actionable.

## Good prompts

### Specific file or function

```
@aizu fix the null pointer in parseConfig() in config/parser.go
```

### Clear scope with context

```
@aizu add input validation to the signup handler.
Reject emails without an @ symbol and passwords shorter than 8 characters.
Add tests for both cases.
```

### Review with direction

```
@aizu review the error handling in internal/worker/worker.go.
Are there any cases where errors are silently swallowed?
```

## Bad prompts

### Too vague

```
@aizu fix the bugs
```

The agent doesn't know which file, which function, or what "the bugs" are.

### Too broad

```
@aizu refactor the codebase to use error wrapping
```

This could touch dozens of files. The agent will likely time out or produce
incomplete changes.

### Missing context

```
@aizu why is it slow?
```

Without pointing to a specific function or test, the agent has no way to
narrow down the issue.

## Tips

1. **Mention file paths.** The agent has full repo access but needs direction.
2. **Describe the expected behavior.** "Should return an error" is better than
   "is broken".
3. **One task per comment.** The agent processes each `@aizu` comment as a
   separate task. Batch requests may be partially fulfilled.
4. **Reference PRs or issues.** On a PR, the agent sees the diff. On an issue,
   it sees the issue body and comments — use that context.
5. **Ask for tests.** Adding "and add tests" to your prompt ensures the agent
   writes test coverage alongside the fix.

## On pull requests vs issues

- **PR comments:** The agent works on the PR's branch and pushes commits
  directly. Use for targeted fixes to existing code.
- **Issue comments:** The agent creates a new branch and opens a PR. Use for
  new features or standalone fixes.

## Iterating

If the first response isn't quite right, follow up in the same thread:

```
@aizu that's close but please also handle the case where the input is nil
```

The agent sees the full conversation history, so it can build on its previous
work without you repeating context.
