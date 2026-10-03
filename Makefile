BINARY := glinet-cli
PKG    := ./src
DIST   := dist

# Coverage profile written by "make cover". Override: make cover COVERAGE_FILE=cover.out
COVERAGE_FILE := coverage.out

# Release targets, built with CGO off. go-keyring needs no cgo on any of them.
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

# The tag without its "v", matching the version Homebrew passes to the build.
# Off a tag it reads like 1.2.3-4-gabcdef0, or a bare commit hash before the
# first tag, with "-dirty" when the tree has changes. Override: make VERSION=1.2.3
ifndef VERSION
VERSION := $(or $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//'),dev)
endif

LDFLAGS := -X main.version=$(VERSION)

# Release builds leave debug data out of the binary: -s drops the symbol
# table, -w the DWARF debug info, -trimpath the local file paths.
RELEASE_FLAGS := -trimpath -ldflags "-s -w $(LDFLAGS)"

# Pinned and built with the local Go, so it always understands go.mod's Go
# version. Override with an installed binary: make lint GOLANGCI_LINT=golangci-lint
GOLANGCI_LINT_VERSION := v2.14.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.8.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo shasum -a 256)

.DEFAULT_GOAL := all
.PHONY: all build release test cover lint vuln check fmt dist publish hooks clean help

all: lint test build ## Lint, test and build

check: lint test vuln ## All CI checks: lint, test and vulnerability scan

build: ## Build ./glinet-cli for this machine
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

release: lint test vuln ## Build ./glinet-cli without debug info
	go build $(RELEASE_FLAGS) -o $(BINARY) $(PKG)

test: ## Run tests with the race detector
	go test -race ./...

cover: ## Run tests with the race detector and print total coverage
	go test -race -coverprofile=$(COVERAGE_FILE) ./...
	go tool cover -func=$(COVERAGE_FILE) | tail -1

lint: ## Run golangci-lint and check that go.mod is tidy
	$(GOLANGCI_LINT) run ./...
	go mod tidy -diff

vuln: ## Check deps and the local Go's standard library for known vulnerabilities
	$(GOVULNCHECK) ./...

fmt: ## Format the code
	$(GOLANGCI_LINT) fmt ./...

dist: ## Build release archives and checksums.txt into dist/
	@rm -rf $(DIST)
	@mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		exe=$(BINARY); [ $$os != windows ] || exe=$(BINARY).exe; \
		echo "build $$name"; \
		mkdir $(DIST)/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(RELEASE_FLAGS) \
			-o $(DIST)/$$name/$$exe $(PKG); \
		cp LICENSE README.md $(DIST)/$$name/; \
		if [ $$os = windows ]; then \
			(cd $(DIST) && zip -qr $$name.zip $$name); \
		else \
			COPYFILE_DISABLE=1 tar --no-xattrs -C $(DIST) -czf $(DIST)/$$name.tar.gz $$name; \
		fi; \
		rm -rf $(DIST)/$$name; \
	done
	cd $(DIST) && $(SHA256) *.tar.gz *.zip > checksums.txt

publish: ## Upload dist/ to a GitHub release for the tag at HEAD
	@set -e; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "publish: commit or stash changes first" >&2; exit 1; \
	fi; \
	tag=$$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null) || { \
		echo "publish: HEAD has no v* tag; run: git tag v1.2.3 && git push origin v1.2.3" >&2; exit 1; }; \
	if [ "$$tag" != "v$(VERSION)" ]; then \
		echo "publish: VERSION=$(VERSION) does not match tag $$tag" >&2; exit 1; \
	fi
	$(MAKE) --no-print-directory test vuln dist
	gh release create v$(VERSION) --verify-tag --generate-notes \
		$(DIST)/*.tar.gz $(DIST)/*.zip $(DIST)/checksums.txt

hooks: ## Use scripts/hooks as the git hooks dir
	git config core.hooksPath scripts/hooks

clean: ## Remove build output
	rm -rf $(BINARY) $(DIST) $(COVERAGE_FILE)

help: ## List targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z]+:.*## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
