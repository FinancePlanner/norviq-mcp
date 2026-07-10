.PHONY: build test run tidy docker

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
