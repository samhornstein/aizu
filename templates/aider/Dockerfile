# Agent sandbox image for the aider engine (AIZU_ENGINE=aider). The worker
# runs the engine command inside a container built from this image, with the
# repo checked out at /workspace/repo. Works with an API key or a local
# OpenAI-compatible model server.
FROM python:3.12-slim-bookworm

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
     bash ca-certificates curl git gnupg ripgrep \
  && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
  && apt-get update \
  && apt-get install -y --no-install-recommends gh \
  && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir aider-chat

# Don't touch the repo's .gitignore (aider adds .aider* to it by default);
# its scratch files stay untracked and are never pushed.
ENV AIDER_GITIGNORE=false

WORKDIR /workspace
