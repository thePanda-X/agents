.PHONY: build install test check demo

GOEXE := $(shell go env GOEXE)

build:
	go build -o bin/agents$(GOEXE) .

install:
	go install .

test:
	go test -race ./...

check:
	go vet ./...
	go test -race ./...

demo:
	go run . --demo
