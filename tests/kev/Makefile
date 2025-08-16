.PHONY: all test clean

all: test

test:
	@echo "=== Running KEV Tests ==="
	@echo "\n--- Unit Tests ---"
	@go test -v .
	@echo "\n--- Project Root Discovery Test (from root) ---"
	@go run cmd/app/main.go
	@echo "\n--- Project Root Discovery Test (from app dir) ---"
	@cd cmd/app && go run main.go
	@echo "\n--- Project Root Discovery Test (from deep nesting) ---"
	@cd cmd/app/nested && go run deep.go

clean:
	@rm -f go.mod go.sum
	@rm -rf cmd
	@echo "Cleaned test project"

init:
	@go mod init test-project
	@go mod edit -replace github.com/adriangalilea/go-utils=/Users/adrian/Developer/go-utils
	@go get github.com/adriangalilea/go-utils
	@echo "Test project initialized"