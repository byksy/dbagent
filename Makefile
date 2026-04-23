.PHONY: build test test-integration lint run docker-up docker-down tidy clean fixtures

BINARY := bin/dbagent

build:
	go build -o $(BINARY) ./cmd/dbagent

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

clean:
	rm -rf bin/
