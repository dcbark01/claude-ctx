BIN := claude-ctx

.PHONY: build test e2e e2e-deps lint install clean

build:
	go build -o $(BIN) .

test:
	go test ./... -race
	@if command -v bun >/dev/null 2>&1; then cd internal/opencode/plugin && bun test; else echo "bun not found; skipping plugin tests"; fi

# End-to-end: real agentgateway + opencode / Claude Code against a mock
# upstream. Tests skip when opencode or claude is not on PATH.
e2e: .bin/agentgateway
	go test -tags e2e -count=1 ./e2e/

e2e-deps .bin/agentgateway:
	./scripts/fetch-agentgateway.sh .bin

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else go vet ./...; fi
	go vet -tags e2e ./e2e/

install:
	go install .

clean:
	rm -f $(BIN)
