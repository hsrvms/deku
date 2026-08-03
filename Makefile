-include .env
export

.PHONY: build test test-race vet mod-verify fmt fmt-check lint run clean cover dev install-dev ci all

BINARY := deku
MODULE := github.com/hsrvms/deku

build:
	go build -o $(BINARY) ./cmd/deku/

test:
	env -u DEKU_PROVIDER_ENDPOINT -u DEKU_PROVIDER_API_KEY -u DEKU_PROVIDER_MODEL go test ./...

test-race:
	env -u DEKU_PROVIDER_ENDPOINT -u DEKU_PROVIDER_API_KEY -u DEKU_PROVIDER_MODEL go test -race ./...

vet:
	go vet ./...

mod-verify:
	go mod verify

fmt:
	gofmt -w -s .

fmt-check:
	@files="$$(gofmt -s -l $$(git ls-files -- '*.go'))"; \
	if [ -n "$$files" ]; then \
		echo "Go files need formatting:"; \
		echo "$$files"; \
		exit 1; \
	fi

lint:
	staticcheck ./...
	golangci-lint run ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) cover.out

cover:
	env -u DEKU_PROVIDER_ENDPOINT -u DEKU_PROVIDER_API_KEY -u DEKU_PROVIDER_MODEL go test -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html
	@echo "coverage report: cover.html"

AIR := $(shell which air 2>/dev/null || echo $(HOME)/go/bin/air)

dev:
	@test -x $(AIR) || { echo "air not found — run: make install-dev"; exit 1; }
	@$(AIR)

install-dev:
	@GOBIN=$${GOBIN:-$$HOME/go/bin} go install github.com/air-verse/air@latest
	@echo "air installed — ensure $${GOBIN:-$$HOME/go/bin} is in your PATH"

ci: fmt-check mod-verify vet test test-race lint build

all: fmt vet test build
