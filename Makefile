GO ?= $(shell command -v go 2>/dev/null)
export GOTOOLCHAIN = local

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS = -s -w -X github.com/ContextOwl/cowl/internal/cli.Version=$(VERSION)
PLATFORMS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test dist

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cowl ./cmd/cowl

test:
	$(GO) vet ./...
	$(GO) test ./...

dist:
	rm -rf dist && mkdir -p dist
	@set -e; for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		bin=cowl; [ "$$os" != "windows" ] || bin=cowl.exe; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$bin ./cmd/cowl; \
		if [ "$$os" = "windows" ]; then \
			$(GO) run ./cmd/release-archive -output dist/cowl_$(VERSION)_$${os}_$${arch}.zip -file dist/$$bin && rm dist/$$bin; \
		else \
			tar -czf dist/cowl_$(VERSION)_$${os}_$${arch}.tar.gz -C dist $$bin && rm dist/$$bin; \
		fi; \
	done
	cd dist && { command -v sha256sum >/dev/null && sha256sum cowl_* || shasum -a 256 cowl_*; } > SHA256SUMS
