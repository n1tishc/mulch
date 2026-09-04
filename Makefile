.PHONY: build test arch lint ui verify run release-dry

build:
	go build ./cmd/mulch

test:
	go test -race ./...

arch:
	go test ./internal -run ImportBoundaries

ui:
	cd ui && npm ci && npm run build

lint:
	golangci-lint run

verify: arch lint ui
	go vet ./...
	go test -race ./...

run:
	go run ./cmd/mulch serve

release-dry:
	goreleaser release --snapshot --clean
