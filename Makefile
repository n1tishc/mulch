.PHONY: build test arch lint ui verify run release-dry docs-html

build:
	go build ./cmd/mulch

test:
	go test -race ./...

arch:
	go test ./internal -run ImportBoundaries

ui:
	cd ui && npm ci && npm test && npm run build

lint:
	golangci-lint run

verify: arch lint ui
	node scripts/check-web-assets.mjs
	go vet ./...
	go test -race ./...

run:
	go run ./cmd/mulch serve

release-dry:
	node scripts/check-web-assets.mjs --tracked
	goreleaser release --snapshot --clean

docs-html:
	node scripts/build-docs-html.mjs

# Deterministic evidence for the context-repair contract; no provider key needed.
.PHONY: contract distribution-test
contract:
	go test -race -count=1 -v ./internal/intervene ./internal/event ./internal/score

distribution-test:
	CGO_ENABLED=0 go build -trimpath -ldflags '-X main.version=0.1.0' -o dist/install-test/mulch ./cmd/mulch
	MULCH_TEST_BINARY=dist/install-test/mulch node --test scripts/distribution.test.mjs

.PHONY: correctness-test
correctness-test:
	go test -race -count=1 ./internal/eval ./internal/runtime ./internal/prompt ./internal/cli

# Install Playwright's Chromium once: cd ui && npx playwright install chromium
.PHONY: web-test
web-test:
	cd ui && npm test && npm run build && npm run test:e2e
