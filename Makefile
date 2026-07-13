.PHONY: fmt vet lint test test-race test-integration build run-dry tidy check

BIN := bin/job-hunt-agent

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration ./...

build:
	go build -o $(BIN) ./cmd/job-hunt-agent

run-dry: build
	$(BIN) run --dry-run

tidy:
	go mod tidy

check: fmt vet lint test
