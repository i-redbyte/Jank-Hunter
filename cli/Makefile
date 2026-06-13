BINARY ?= jankhunter
DIST ?= dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS ?= darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: test build release checksums clean

test:
	go test ./...

build:
	mkdir -p bin
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/jankhunter

release: clean test
	mkdir -p $(DIST)
	@set -eu; \
	for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		out="$(DIST)/$(BINARY)_$(VERSION)_$${GOOS}_$${GOARCH}"; \
		mkdir -p "$$out"; \
		GOOS=$$GOOS GOARCH=$$GOARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$$out/$(BINARY)" ./cmd/jankhunter; \
		tar -C "$$out" -czf "$$out.tar.gz" "$(BINARY)"; \
		rm -rf "$$out"; \
	done
	$(MAKE) checksums

checksums:
	shasum -a 256 $(DIST)/*.tar.gz > $(DIST)/checksums.txt

clean:
	rm -rf bin $(DIST)
