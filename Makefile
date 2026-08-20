.PHONY: build test run tidy docker hooks fmt lint

build:
	go build ./...

test:
	go test ./...

run:
	go run ./cmd/norviq-mcp

tidy:
	go mod tidy

docker:
	docker build -t norviq-mcp:dev .

hooks: ## Install versioned git hooks (.githooks → core.hooksPath)
	bash scripts/install-hooks.sh

fmt: ## Format Go sources with gofumpt
	go tool gofumpt -w .

lint: ## Run golangci-lint (same command the pre-commit hook and CI run)
	golangci-lint run
