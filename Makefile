.PHONY: build test test-integration lint run docker-up docker-down tidy clean fixtures rule-fixtures schema-fixtures

BINARY := bin/dbagent

# VERSION is produced by git describe: "vX.Y.Z" on a tagged commit,
# "vX.Y.Z-N-gSHA" N commits after the tag, with "-dirty" suffix if
# the working tree has uncommitted changes. Shallow clones or a
# tagless repo fall back to "dev" so the build still succeeds.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/byksy/dbagent/internal/cli.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/dbagent

test:
	go test -race ./...

test-integration:
	go test -race -tags=integration ./...

lint:
	go vet ./...

run: build
	./$(BINARY) $(ARGS)

docker-up:
	docker compose up -d

docker-down:
	docker compose down

tidy:
	go mod tidy

fixtures:
	./scripts/capture-fixtures.sh

rule-fixtures:
	./scripts/capture-rules-fixtures.sh

schema-fixtures:
	./scripts/capture-schema-fixtures.sh

clean:
	rm -rf bin/
