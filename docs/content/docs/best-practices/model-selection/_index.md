---
title: "Model Selection"
weight: 2
---

# Model Selection

## Local models (llama.cpp)

**Best for:** privacy, zero cost, offline use. Use `Q4_K_M` quantization for
the best speed/quality ratio.

| Size | Quality | RAM needed |
|------|---------|------------|
| 1.5B | Basic | ~4 GB |
| 7B | Good | ~8 GB |
| 14B | Strong | ~16 GB |

The 1.5B model is useful for testing the pipeline but may produce incoherent
output for complex tasks.

## Cloud models (API keys)

**Best for:** quality and reliability. Set `ANTHROPIC_API_KEY` or
`OPENAI_API_KEY` in `.env`.

**Recommended:** Claude 3.5 Sonnet, GPT-4o, or GPT-4o mini for budget tasks.

**Cost tips:**

- A single run uses 10K–50K tokens depending on repo size
- Use `AIZU_TIMEOUT` to cap runaway runs
- Monitor your API usage dashboard

## Timeout tuning

Match the timeout to your model:

```toml
[agent]
timeout = 600   # 10 min — 7B+ local models
timeout = 300   # 5 min — cloud models
timeout = 120   # 2 min — 1.5B local models
```
