.PHONY: build run test fmt vet lint check tidy clean

# Dev-loop targets. Assumes `go`, `golangci-lint`, etc. are on PATH -
# `nix develop` (or direnv) provides them. For release builds use `nix build`.

BIN := sergeant
PKG := ./cmd/sergeant

build:
	go build -o $(BIN) $(PKG)

run: build
	./$(BIN)

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

check: fmt vet lint test

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
