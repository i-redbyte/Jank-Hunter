BINARY ?= jankhunter
BIN_DIR ?= bin
DIST ?= dist
TOOLS_DIR ?= .tools
GO_VERSION ?= 1.22.12
VERSION ?= 1.0.0
PLATFORMS ?= darwin/amd64 darwin/arm64 linux/amd64 linux/arm64
PREFIX ?= /usr/local

HOST_OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
HOST_ARCH := $(shell uname -m | sed -e 's/^x86_64$$/amd64/' -e 's/^aarch64$$/arm64/')
BUILD_OS ?= $(if $(filter darwin,$(HOST_OS)),darwin,$(if $(filter linux,$(HOST_OS)),linux,$(HOST_OS)))
BUILD_ARCH ?= $(HOST_ARCH)
TARGET_EXT = $(if $(filter windows,$(BUILD_OS)),.exe,)
OUT ?= $(BIN_DIR)/$(BINARY)$(TARGET_EXT)

SYSTEM_GO := $(shell command -v go 2>/dev/null)
BOOTSTRAP_GO := $(TOOLS_DIR)/go/bin/go
GO_CMD := $(if $(SYSTEM_GO),$(SYSTEM_GO),$(BOOTSTRAP_GO))

.PHONY: bootstrap-go deps test build install release checksums clean

bootstrap-go:
	@if command -v go >/dev/null 2>&1; then \
		echo "using Go: $$(go version)"; \
		exit 0; \
	fi; \
	if [ -x "$(BOOTSTRAP_GO)" ]; then \
		echo "using bundled Go: $$($(BOOTSTRAP_GO) version)"; \
		exit 0; \
	fi; \
	host_os="$$(uname -s | tr '[:upper:]' '[:lower:]')"; \
	case "$$host_os" in \
		darwin) goos="darwin" ;; \
		linux) goos="linux" ;; \
		*) echo "unsupported OS for automatic Go bootstrap: $$host_os" >&2; exit 1 ;; \
	esac; \
	host_arch="$$(uname -m)"; \
	case "$$host_arch" in \
		x86_64|amd64) goarch="amd64" ;; \
		arm64|aarch64) goarch="arm64" ;; \
		*) echo "unsupported CPU for automatic Go bootstrap: $$host_arch" >&2; exit 1 ;; \
	esac; \
	archive="go$(GO_VERSION).$$goos-$$goarch.tar.gz"; \
	url="https://go.dev/dl/$$archive"; \
	tmp="$(TOOLS_DIR)/$$archive"; \
	mkdir -p "$(TOOLS_DIR)"; \
	echo "Go is not installed. Downloading $$url"; \
	if command -v curl >/dev/null 2>&1; then \
		curl -fL "$$url" -o "$$tmp"; \
	elif command -v wget >/dev/null 2>&1; then \
		wget -O "$$tmp" "$$url"; \
	else \
		echo "curl or wget is required to download Go automatically" >&2; exit 1; \
	fi; \
	rm -rf "$(TOOLS_DIR)/go"; \
	tar -C "$(TOOLS_DIR)" -xzf "$$tmp"; \
	"$(BOOTSTRAP_GO)" version

deps: bootstrap-go
	"$(GO_CMD)" mod download
	"$(GO_CMD)" mod verify

test: deps
	"$(GO_CMD)" test ./...

build: deps
	mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 GOOS="$(BUILD_OS)" GOARCH="$(BUILD_ARCH)" "$(GO_CMD)" build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(OUT)" ./cmd/jankhunter
	@echo "built $(OUT)"

install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 0755 "$(OUT)" "$(DESTDIR)$(PREFIX)/bin/$(BINARY)$(TARGET_EXT)"
	@echo "installed $(DESTDIR)$(PREFIX)/bin/$(BINARY)$(TARGET_EXT)"

release: clean test
	mkdir -p "$(DIST)"
	@set -eu; \
	for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		ext=""; \
		if [ "$$GOOS" = "windows" ]; then ext=".exe"; fi; \
		out="$(DIST)/$(BINARY)_$(VERSION)_$${GOOS}_$${GOARCH}"; \
		mkdir -p "$$out"; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH "$(GO_CMD)" build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$$out/$(BINARY)$$ext" ./cmd/jankhunter; \
		tar -C "$$out" -czf "$$out.tar.gz" "$(BINARY)$$ext"; \
		rm -rf "$$out"; \
	done
	$(MAKE) checksums

checksums:
	@if command -v shasum >/dev/null 2>&1; then \
		shasum -a 256 "$(DIST)"/*.tar.gz > "$(DIST)/checksums.txt"; \
	else \
		sha256sum "$(DIST)"/*.tar.gz > "$(DIST)/checksums.txt"; \
	fi

clean:
	rm -rf "$(BIN_DIR)" "$(DIST)"
