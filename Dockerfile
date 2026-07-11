# ── Build stage ──────────────────────────────────────────────────
FROM golang:1-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/aizu .

# ── Runtime stage ────────────────────────────────────────────────
FROM alpine:3.21

# docker-cli lets the worker launch agent sandboxes; git is used during clone.
RUN apk add --no-cache docker-cli git ca-certificates

COPY --from=builder /bin/aizu /usr/local/bin/aizu

ENTRYPOINT ["aizu"]
