.PHONY: build run vet fmt mod-tidy test test-e2e clean build-agent up down docs-serve docs-build install-hooks

build: vet
	go build -o bin/aizu .

vet:
	go vet ./...

fmt:
	gofmt -w .

mod-tidy:
	go mod tidy

test:
	go test ./...

test-e2e:
	go test -tags e2e -race -timeout 60s ./e2e/...

run: build
	./bin/aizu

clean:
	rm -rf bin/

install-hooks:
	ln -sf ../../.githooks/pre-commit .git/hooks/pre-commit
	ln -sf ../../.githooks/commit-msg .git/hooks/commit-msg

# Build the agent sandbox image (aizu-agent:pi) the worker runs agents in.
build-agent:
	docker compose build agent

# Build the agent image, then start Aizu + Redis.
up: build-agent
	docker compose up -d

down:
	docker compose down

docs-serve:
	cd docs && hugo server -D

docs-build:
	cd docs && hugo
