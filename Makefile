.PHONY: build test vet fmt run clean all

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
	rm -f $(BINARY)

all: fmt vet test build