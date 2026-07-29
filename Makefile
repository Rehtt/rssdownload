APP_NAME := rssdownload
WEB_DIR := web
OUTPUT_DIR := dist
VERSION ?= dev
COMMIT ?= unknown
BUILD_TIME ?= unknown
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

.PHONY: dev dev-api dev-web test test-go test-web build clean

dev:
	@echo "Run 'make dev-api' and 'make dev-web' in separate terminals."

dev-api:
	go run -buildvcs=false ./cmd/rssdownload

dev-web:
	pnpm --dir $(WEB_DIR) dev

test: test-go test-web

test-go:
	go test ./...

test-web:
	pnpm --dir $(WEB_DIR) test -- --run

build:
	rm -rf internal/webui/dist/assets
	rm -f internal/webui/dist/index.html
	pnpm --dir $(WEB_DIR) build
	mkdir -p $(OUTPUT_DIR)
	go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(APP_NAME) ./cmd/rssdownload

clean:
	go clean -testcache
	rm -rf $(OUTPUT_DIR) $(WEB_DIR)/dist internal/webui/dist/assets
	rm -f internal/webui/dist/index.html
