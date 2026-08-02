.PHONY: build test vet fmt run clean cover dev install-dev all

BINARY := deku
MODULE := github.com/hsrvms/deku

build:
	go build -o $(BINARY) ./cmd/deku/

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w -s .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) cover.out

cover:
	go test -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html
	@echo "coverage report: cover.html"

AIR := $(shell which air 2>/dev/null || echo $(HOME)/go/bin/air)

dev:
	@test -x $(AIR) || { echo "air not found — run: make install-dev"; exit 1; }
	@$(AIR)

install-dev:
	@GOBIN=$${GOBIN:-$$HOME/go/bin} go install github.com/air-verse/air@latest
	@echo "air installed — ensure $${GOBIN:-$$HOME/go/bin} is in your PATH"

all: fmt vet test build