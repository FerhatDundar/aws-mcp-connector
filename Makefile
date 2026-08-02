.PHONY: build test vet fmt lint tidy sandbox-up sandbox-down clean

BINARY := aws-connector-server
SRC_DIR := go-server

build: ## Build the server binary
	cd $(SRC_DIR) && go build -o $(BINARY) .
	cp $(SRC_DIR)/$(BINARY) plugin/servers/go/$(BINARY)

test: ## Run tests
	cd $(SRC_DIR) && go test ./... -v

vet: ## go vet
	cd $(SRC_DIR) && go vet ./...

fmt: ## Check formatting (fails if gofmt would change anything)
	@test -z "$$(gofmt -l $(SRC_DIR))" || (echo "gofmt needs to be run on:"; gofmt -l $(SRC_DIR); exit 1)

lint: ## Run golangci-lint (requires it installed locally)
	cd $(SRC_DIR) && golangci-lint run ./...

tidy: ## go mod tidy
	cd $(SRC_DIR) && go mod tidy

sandbox-up: ## Start whatever local test double this connector uses (LocalStack, a mock server, etc.) — see SETUP.md
	docker compose up -d

sandbox-down: ## Stop the local test double
	docker compose down

clean: ## Remove built binaries
	rm -f $(SRC_DIR)/$(BINARY) plugin/servers/go/$(BINARY)
