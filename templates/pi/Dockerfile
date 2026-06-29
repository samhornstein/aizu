# Agent sandbox image. The worker runs the configured [agent].command inside a
# container built from this image, with the repo checked out at /workspace/repo.
FROM node:24-bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
     bash ca-certificates git ripgrep \
  && rm -rf /var/lib/apt/lists/*

# Install the pi coding agent (the default engine; swap for any agent you like).
RUN npm install -g --ignore-scripts @earendil-works/pi-coding-agent

WORKDIR /workspace

ENTRYPOINT ["pi"]
