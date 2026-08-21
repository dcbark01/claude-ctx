BIN := claude-ctx

.PHONY: build test lint install clean

build:
	go build -o $(BIN) .

test:
	go test ./... -race

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else go vet ./...; fi

install:
	go install .

clean:
	rm -f $(BIN)
