# Run `make` to see the available commands. Requires Go and a POSIX shell.
.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
ARGS ?=

# Install locally by default; override these for system installs or packaging.
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=

.PHONY: help build run test test-race vet fmt fmt-check check coverage tidy install uninstall clean

help: ## Show available commands
	@printf 'Meetlet development commands:\n\n'
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build bin/meetlet
	$(GO) build -o bin/meetlet .

run: build ## Build and run; pass CLI arguments with ARGS='...'
	./bin/meetlet $(ARGS)

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run all tests with the race detector (requires a C compiler)
	$(GO) test -race ./...

vet: ## Check for common Go mistakes
	$(GO) vet ./...

fmt: ## Format Go source files
	$(GOFMT) -w .

fmt-check: ## Check formatting without changing files
	@files="$$($(GOFMT) -l .)" || exit $$?; \
	if [ -n "$$files" ]; then \
		printf 'Run make fmt to format these files:\n%s\n' "$$files"; \
		exit 1; \
	fi

check: fmt-check vet test-race ## Run formatting, vet, and race checks (also used by CI)

coverage: ## Run tests and print coverage; save coverage.out
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

tidy: ## Update go.mod and go.sum after dependency changes
	$(GO) mod tidy

install: build ## Install to PREFIX/bin (default: ~/.local/bin)
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 bin/meetlet "$(DESTDIR)$(BINDIR)/meetlet"
	@printf 'Installed meetlet to %s\n' "$(DESTDIR)$(BINDIR)/meetlet"

uninstall: ## Remove the installed executable (uses the same install variables)
	rm -f "$(DESTDIR)$(BINDIR)/meetlet"

clean: ## Remove the built executable and coverage report
	rm -f bin/meetlet coverage.out
