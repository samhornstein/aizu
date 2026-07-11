# Agent sandbox image. The worker runs the configured [agent].command inside a
# container built from this image, with the repo checked out at /workspace/repo.
FROM node:24-bookworm-slim

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

# Install the pi coding agent (the default engine; swap for any agent you like).
RUN npm install -g --ignore-scripts @earendil-works/pi-coding-agent

WORKDIR /workspace
