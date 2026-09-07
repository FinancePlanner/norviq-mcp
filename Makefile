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

catalog-snapshot: ## Refresh internal/tools/testdata/action-catalog.json from a backend
	@# The Go tool structs are hand-written against the same API the backend's
	@# ActionCatalog serves, so the snapshot is what keeps the two comparable.
	@# NORVIQ_TOKEN needs any authenticated credential; the endpoint returns
	@# capability metadata about the caller's own account.
	@test -n "$$NORVIQ_TOKEN" || { echo "set NORVIQ_TOKEN (any valid PAT or session JWT)"; exit 1; }
	curl -fsS -H "Authorization: Bearer $$NORVIQ_TOKEN" \
		"$${NORVIQ_API:-https://api.norviq.org}/v1/actions/catalog" \
		| python3 -c 'import sys,json; d=json.load(sys.stdin); \
			print(json.dumps({"actions":[{"name":a["name"],"destructive":a["destructive"]} \
			for a in sorted(d["actions"], key=lambda x: x["name"])]}, indent=2))' \
		> internal/tools/testdata/action-catalog.json
	@echo "refreshed; run 'make test' to check parity"
