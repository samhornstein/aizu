.PHONY: build run vet fmt mod-tidy test test-integration clean build-agent up down docs-serve docs-build install-hooks

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

test-integration:
	go test -race -tags=integration ./...

run: build
	./bin/aizu

clean:
	rm -rf bin/

install-hooks:
	ln -sf ../../.githooks/pre-commit .git/hooks/pre-commit
	ln -sf ../../.githooks/commit-msg .git/hooks/commit-msg

# Build the agent container image referenced by aizu.toml ([agent].image).
build-agent:
	docker build -t aizu-agent:pi -f templates/pi/Dockerfile .

up:
	docker compose up -d

down:
	docker compose down

docs-serve:
	cd docs && hugo server -D

docs-build:
	cd docs && hugo
