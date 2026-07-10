# Feature: prebuilt images — install without cloning

**Branch:** `feat/prebuilt-images`
**PR title:** `feat: publish images to ghcr.io and add a no-clone quickstart`

## Context

Today's install path is: clone the repo, `docker compose build agent`,
`docker compose up -d --build`. That means every new user compiles Go and
builds two images before seeing anything work — several minutes of friction
and a class of "build failed on my machine" support issues.

The goal quickstart is: download one `docker-compose.yml`, write two lines
of `.env`, `docker compose up -d`. That requires publishing both images:

- `ghcr.io/samhornstein/aizu` — the poller/worker (root `Dockerfile`)
- `ghcr.io/samhornstein/aizu-agent-pi` — the agent sandbox
  (`templates/pi/Dockerfile`)

The release workflow (`.github/workflows/release.yml`) currently tags +
creates a GitHub Release only; its header comment explicitly says no images
are published — that comment must be updated too. Note prior history: the
project *moved away from* prebuilt images to build-from-source (commit
`96b694e`). This plan reintroduces publishing as the **user-facing default**
while keeping build-from-source as the contributor path — both compose
paths must keep working.

## Approach

### Steps

1. **Add an image-publish job to `release.yml`**, after the tag step:

   ```yaml
   permissions:
     contents: write
     packages: write   # add
   ```

   New steps in the release job (or a second job needing the tag output):

   ```yaml
   - uses: docker/setup-qemu-action@v3
   - uses: docker/setup-buildx-action@v3
   - uses: docker/login-action@v4
     with:
       registry: ghcr.io
       username: ${{ github.actor }}
       password: ${{ secrets.GITHUB_TOKEN }}
   - uses: docker/build-push-action@v6
     with:
       context: .
       platforms: linux/amd64,linux/arm64
       push: true
       tags: |
         ghcr.io/samhornstein/aizu:latest
         ghcr.io/samhornstein/aizu:${{ steps.ver.outputs.next }}
   - uses: docker/build-push-action@v6
     with:
       context: ./templates/pi
       platforms: linux/amd64,linux/arm64
       push: true
       tags: |
         ghcr.io/samhornstein/aizu-agent-pi:latest
         ghcr.io/samhornstein/aizu-agent-pi:${{ steps.ver.outputs.next }}
   ```

   Checkout must happen at the new tag (it already checks out the released
   commit — verify the build runs after tagging, in the same job, so the
   source matches the tag). Both platforms matter: target users are on
   Apple Silicon (arm64) and Linux x86. Update the workflow's header
   comment (lines 8-10) which currently states no images are published.
   After the first release, make both GHCR packages **public** in the repo's
   Packages settings (one-time manual step — say so in the PR description).

2. **Ship a standalone compose file for users.** Add `deploy/docker-compose.yml`
   (kept out of the repo root so it can't collide with the dev compose):
   same shape as the existing `docker-compose.yml` but with
   `image: ghcr.io/samhornstein/aizu:latest` instead of `build: .`, no
   `agent` service (nothing to build), and the same Redis service, socket
   mount, `env_file`, and `REDIS_URL` default. Add a comment header:
   "Download this file and run `docker compose up -d` — see README."

3. **Make the agent image default resolvable.** In
   `internal/config/config.go`, change the `ContainerImage` default from
   `aizu-agent:pi` to `ghcr.io/samhornstein/aizu-agent-pi:latest` (`docker
   run` pulls it automatically on first use — no compose involvement
   needed). Keep the local dev flow working: in the dev
   `docker-compose.yml`, set `CONTAINER_IMAGE=aizu-agent:pi` in the aizu
   service's `environment:` block so source builds keep using the locally
   built agent image. Update `.env.example`'s CONTAINER_IMAGE comment.

4. **Rewrite the README quickstart** around the no-clone path:

   ```sh
   mkdir aizu && cd aizu
   curl -fsSLO https://raw.githubusercontent.com/samhornstein/aizu/main/deploy/docker-compose.yml
   cat > .env <<EOF
   GITHUB_TOKEN=ghp_...
   AIZU_REPOS=owner/repo
   EOF
   docker compose up -d
   ```

   Move clone + `docker compose build` into a "Developing / building from
   source" section (or point at `docs/content/docs/development/`). Update
   `docs/content/docs/getting-started/_index.md` to match.

## Files to modify

- `.github/workflows/release.yml`
- `deploy/docker-compose.yml` (new)
- `internal/config/config.go` (+ its test for the new default)
- `docker-compose.yml` (pin CONTAINER_IMAGE for dev)
- `.env.example`, `README.md`, `docs/content/docs/getting-started/_index.md`,
  `docs/content/docs/development/_index.md`

## Tests

- `internal/config/config_test.go`: default `ContainerImage` equals the ghcr
  reference; env override still wins.
- Workflow YAML can't be unit-tested; validate with
  `docker buildx build --platform linux/amd64,linux/arm64 .` and the same
  for `templates/pi` locally (build only, no push) to prove both Dockerfiles
  are multi-arch clean (the Go build is `CGO_ENABLED=0`, fine; the pi image
  is npm-based, fine).

## Verification

```sh
make build && go test -race ./...
docker buildx build --platform linux/amd64,linux/arm64 -t aizu:multiarch-check .
docker buildx build --platform linux/amd64,linux/arm64 -t aizu-agent:multiarch-check templates/pi
docker compose config   # dev compose still valid
docker compose -f deploy/docker-compose.yml config   # user compose valid
```

Full check after merging: run the Release workflow, then on a clean machine
(or empty dir) execute the new README quickstart verbatim and trigger a
task.

## Out of scope

- No Homebrew/apt packaging, no standalone-binary distribution.
- No CI publishing on every push (images publish on releases only).
- Do not remove the build-from-source path.
