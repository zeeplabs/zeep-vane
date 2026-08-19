.PHONY: build test lint vet

build:
	go build -o bin/vane ./cmd/vane

test:
	go test ./...

lint:
	gofmt -l .

vet:
	go vet ./...
