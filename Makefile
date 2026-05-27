# ---- Project settings ----
OUTPUT_NAME ?= xrouter
MAIN        ?= ./cmd/xrouter
DEST        ?= bin

# Keep Docker Go version aligned with go.mod (go 1.25.3)
GO_IMAGE    ?= golang:1.25.3

# Build metadata
# VERSION can be passed: make build VERSION=0.3.0
VERSION     ?= 0.3.0
LDFLAGS     ?= -s -w -X 'main.version=$(VERSION)'

# Persistent caches (inside repo so everything lives under ~/xrouter)
CACHE_DIR   ?= .cache
MODCACHE    ?= $(PWD)/$(CACHE_DIR)/go-mod
GOCACHE     ?= $(PWD)/$(CACHE_DIR)/go-build

# Config file default (change to your preferred location)
CONFIG      ?= $(HOME)/xrouter/config/xrouter/config.yaml

# ---- archive ----
.PHONY: zipxrouter

ARCHIVE_TS := $(shell date '+%Y-%m-%d_%H-%M-%S')
ARCHIVE_NAME := xrouter-source-$(ARCHIVE_TS).zip

zipxrouter:
	zip -r "$(HOME)/Desktop/$(ARCHIVE_NAME)" . \
		-x ".git/*" \
		-x "dist/*" \
		-x "bin/*" \
		-x ".cache/*" \
		-x ".DS_Store" \
		-x "*/.DS_Store" \
		-x "*.log" \
		-x "*/*.log" \
		-x "*/*/*.log" \
		-x "*/*/*/*.log" \
		-x "*.swp" \
		-x "*/*.swp" \
		-x "*/*/*.swp" \
		-x "*/*/*/*.swp"
	@echo "Created  $(HOME)/Desktop/$(ARCHIVE_NAME)"
	
# ---- Helpers ----
.PHONY: help
help:
	@echo "Targets:"
	@echo "  make build              - Build natively on this Mac"
	@echo "  make build-docker       - Build in Docker (reproducible; caches persisted in .cache/)"
	@echo "  make vendor             - Create/update vendor/ directory"
	@echo "  make build-vendor       - Build using vendor/ (regenerates vendor/)"
	@echo "  make build-offline      - Build strictly from vendor/ (expects vendor/ exists)"
	@echo "  make fmt                - gofmt all .go files"
	@echo "  make vet                - go vet ./..."
	@echo "  make test               - go test ./..."
	@echo "  make run-up             - sudo run: xrouter up"
	@echo "  make run-down           - sudo run: xrouter down"
	@echo "  make run-status         - sudo run: xrouter status"
	@echo "  make run-daemon         - sudo run: xrouter run"
	@echo "  make clean              - Remove bin/ and .cache/"
	@echo ""
	@echo "Vars:"
	@echo "  VERSION=0.3.1           - injected into main.version"
	@echo "  CONFIG=.../config.yaml  - config path used by run-* targets"
	@echo ""
	@echo "How to use it:"
	@echo ""
	@echo "Normal local build:"
	@echo ""
	@echo "make build"
	@echo "./bin/xrouter -h"
	@echo ""
	@echo "Reproducible Docker build (with persistent caches in .cache/):"
	@echo ""
	@echo "make build-docker"
	@echo "./bin/xrouter -version"
	@echo ""
	@echo "Prepare offline build:"
	@echo "make vendor"
	@echo "make build-offline"
	@echo ""
	@echo "Notes (important)"
	@echo ""
	@echo "The Docker build uses CGO_ENABLED=0 so cross-compiling darwin/amd64 works from a Linux container."
	@echo "If later you decide to build darwin/arm64 for Apple Silicon, change GOARCH=arm64 in build-docker."

.PHONY: prepare
prepare:
	@mkdir -p "$(DEST)"
	@mkdir -p "$(MODCACHE)"
	@mkdir -p "$(GOCACHE)"

# ---- Native build (Mac) ----
.PHONY: build
build: prepare
	go build -ldflags "$(LDFLAGS)" -o "$(DEST)/$(OUTPUT_NAME)" "$(MAIN)"

# ---- Docker build (Mac host, Linux container) ----
# Note: CGO_ENABLED=0 is required for cross-compile darwin from linux container.
.PHONY: build-docker
build-docker: prepare
	docker run --rm \
	  -e CGO_ENABLED=0 \
	  -e GOOS=darwin \
	  -e GOARCH=amd64 \
	  -e GOTOOLCHAIN=local \
	  -e GOMODCACHE=/go/pkg/mod \
	  -e GOCACHE=/go-build-cache \
	  -v "$(PWD)":/src \
	  -v "$(MODCACHE)":/go/pkg/mod \
	  -v "$(GOCACHE)":/go-build-cache \
	  -w /src \
	  $(GO_IMAGE) \
	  sh -lc 'go build -ldflags "$(LDFLAGS)" -o "/src/$(DEST)/$(OUTPUT_NAME)" "$(MAIN)" && chmod +x "/src/$(DEST)/$(OUTPUT_NAME)"'

# ---- Vendor workflow ----
.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor

.PHONY: build-vendor
build-vendor: prepare vendor
	GOTOOLCHAIN=local GOFLAGS=-mod=vendor go build -ldflags "$(LDFLAGS)" -o "$(DEST)/$(OUTPUT_NAME)" "$(MAIN)"

# This expects vendor/ already exists. It does NOT run tidy/vendor again.
.PHONY: build-offline
build-offline: prepare
	@echo "Building offline from vendor/ ..."
	GOTOOLCHAIN=local GOFLAGS=-mod=vendor go build -ldflags "$(LDFLAGS)" -o "$(DEST)/$(OUTPUT_NAME)" "$(MAIN)"

# ---- Quality checks ----
.PHONY: fmt
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test:
	go test ./...

# ---- Run helpers (require sudo because pfctl/ifconfig/sysctl/dnsmasq) ----
.PHONY: run-up
run-up: build
	@echo "Running: sudo ./$(DEST)/$(OUTPUT_NAME) -config $(CONFIG) up"
	sudo ./$(DEST)/$(OUTPUT_NAME) -config "$(CONFIG)" up

.PHONY: run-down
run-down: build
	@echo "Running: sudo ./$(DEST)/$(OUTPUT_NAME) -config $(CONFIG) down"
	sudo ./$(DEST)/$(OUTPUT_NAME) -config "$(CONFIG)" down

.PHONY: run-status
run-status: build
	@echo "Running: sudo ./$(DEST)/$(OUTPUT_NAME) -config $(CONFIG) status"
	sudo ./$(DEST)/$(OUTPUT_NAME) -config "$(CONFIG)" status

.PHONY: run-daemon
run-daemon: build
	@echo "Running: sudo ./$(DEST)/$(OUTPUT_NAME) -config $(CONFIG) run"
	sudo ./$(DEST)/$(OUTPUT_NAME) -config "$(CONFIG)" run

# ---- Cleanup ----
.PHONY: clean
clean:
	rm -rf "$(DEST)" "$(CACHE_DIR)"

