---
title: "Model Selection"
weight: 2
---

# Model Selection

The quality of Aizu's responses depends entirely on the underlying model.
Choose based on your latency, cost, and capability requirements.

## Local models (llama.cpp)

**Best for:** privacy, zero cost, offline use

```bash
# Small — fast but limited reasoning (~1 GB)
llama-server -hf bartowski/Qwen2.5-Coder-1.5B-Instruct-GGUF:Q4_K_M --port 8080

# Medium — good balance (~4 GB)
llama-server -hf bartowski/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M --port 8080

# Large — best quality, needs GPU or lots of RAM (~8 GB)
llama-server -hf bartowski/Qwen2.5-Coder-14B-Instruct-GGUF:Q4_K_M --port 8080
```

**Trade-offs:**

| Size | Speed | Quality | RAM needed |
|------|-------|---------|------------|
| 1.5B | Fast | Basic | ~4 GB |
| 7B | Moderate | Good | ~8 GB |
| 14B | Slow | Strong | ~16 GB |

**Tips:**

- Use `Q4_K_M` quantization for the best speed/quality ratio
- For larger models, consider `Q3_K_M` if memory is tight
- The 1.5B model is useful for pipeline testing but may produce incoherent
  output for complex tasks

## Cloud models (API keys)

**Best for:** quality, reliability, no hardware requirements

Set one of these in `.env`:

```env
# Anthropic Claude
ANTHROPIC_API_KEY=sk-ant-...

# OpenAI GPT
OPENAI_API_KEY=sk-...
```

**Recommended models:**

- **Claude 3.5 Sonnet** — excellent for code generation and reasoning
- **GPT-4o** — strong all-rounder, fast and cost-effective
- **GPT-4o mini** — budget-friendly for simple tasks

**Cost considerations:**

- A single agent run may use 10K–50K tokens depending on repo size and task
- Use `AIZU_TIMEOUT` to cap runaway runs and control costs
- Monitor your API usage dashboard for unexpected spikes

## Hybrid approach

Use a local model for development/testing and an API key for production:

```env
# .env.development
OPENAI_BASE_URL=http://host.docker.internal:8080/v1

# .env.production
ANTHROPIC_API_KEY=sk-ant-...
```

Switch between them by loading the appropriate env file:

```bash
docker compose --env-file .env.production up -d
```

## Model-specific tuning

### Timeout

Larger models are slower. Increase the timeout accordingly:

```toml
[agent]
timeout = 600   # 10 minutes for 7B+ local models
timeout = 300   # 5 minutes for cloud models
timeout = 120   # 2 minutes for 1.5B local models
```

### Context window

Aizu sends the full conversation history and relevant file contents to the
model. For large repos:

- Scope tasks to specific files or functions
- Use `@aizu` with specific file paths in your prompt
- Consider the `users` filter to limit which comments trigger the agent
