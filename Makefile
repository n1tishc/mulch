.PHONY: build test lint ui run release-dry

build:
	go build ./cmd/mulch

test:
	go test -race ./...

ui:
	cd ui && npm ci && npm run build

lint:
	golangci-lint run

run:
	go run ./cmd/mulch serve

release-dry:
	goreleaser release --snapshot --clean
