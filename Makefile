BINARY      := astl
BUILD_DIR   := bin
PKG         := ./...
MAIN_PACKAGE ?= ./cmd/astl
EXAMPLES    ?= examples

# Quality gates. Ratchets: raise as the repo improves, never lower to green a build.
# COVER_MIN is the coverage floor. Current total is 85.0%, which meets the 85
# target set when the floor was 80. Worst own package is internal/parse at
# 69.7%; raise the floor again once that is closer to the rest.
COVER_MIN ?= 83

# The compatibility corpus lives outside this repo (it carries GPL ansible-lint
# test data; ansible-static-lint itself stays MIT). Override to point elsewhere.
PARITY_REPO ?= ../astl-compatibility-check

# Set to 1 to let parity and bench skip when PARITY_REPO is absent instead of
# failing. The default is to fail, because in a log a gate that silently passed
# without running is indistinguishable from one that ran and agreed, and these
# two are the only checks that see the compatibility invariant at all.
ALLOW_MISSING_PARITY ?= 0

# in_parity_repo <make-target> <name>: run a target in the compatibility repo,
# or explain precisely why it could not run. Shared so parity and bench cannot
# drift apart on the part that decides whether a gate counts as having run.
define in_parity_repo
	@if [ -d "$(PARITY_REPO)" ]; then \
		ASTL_REPO="$(CURDIR)" $(MAKE) -C "$(PARITY_REPO)" $(1); \
	elif [ "$(ALLOW_MISSING_PARITY)" = "1" ]; then \
		echo "SKIP $(2): no compatibility repo at $(PARITY_REPO) (ALLOW_MISSING_PARITY=1)"; \
	else \
		echo "FAIL $(2): no compatibility repo at $(PARITY_REPO), so the check did not run." >&2; \
		echo "  clone it beside this repo:" >&2; \
		echo "    git clone https://github.com/arhuman/astl-compatibility-check $(PARITY_REPO)" >&2; \
		echo "  or point PARITY_REPO=<path> at an existing checkout," >&2; \
		echo "  or set ALLOW_MISSING_PARITY=1 to skip it knowingly." >&2; \
		exit 1; \
	fi
endef

# Pinned tool versions. Keep equal to the 10x _shared/references/versions.md table.
GOLANGCI_VERSION    ?= v2.13.1
GOVULNCHECK_VERSION ?= v1.7.0

# Version metadata injected via ldflags. VERSION_PKG declares Version, GitCommit
# and BuildDate; for a single-binary CLI that is package main itself.
VERSION_PKG ?= main
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
# Committer date, not wall-clock, so rebuilds of one commit stay byte-identical.
BUILD_DATE  ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X '$(VERSION_PKG).Version=$(VERSION)' \
	-X '$(VERSION_PKG).GitCommit=$(COMMIT)' \
	-X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)'

.DEFAULT_GOAL := help
.PHONY: audit bench build check ci clean cover fuzz help parity perfguard release test tidy tools

## audit: run quality control checks (mod verify, lint, vuln scan, coverage gate)
audit: cover
	@which golangci-lint > /dev/null || $(MAKE) tools
	@which govulncheck > /dev/null || $(MAKE) tools
	go mod verify
	golangci-lint run $(PKG)
	govulncheck $(PKG)

## bench: speed regression guards, corpus under 150 ms plus the noqa shape guard
# The corpus guard lives in the compatibility repo beside the corpus it
# measures; this target only delegates for it, like parity. Override the budget
# there with BENCH_BUDGET_MS.
bench: perfguard
	$(call in_parity_repo,bench,bench)

## perfguard: assert noqa resolution is still linear in the suppression count
# Tagged out of the ordinary suite because it reads wall-clock time: run inside
# `make cover`, one noisy CI runner failed the coverage gate and the linters
# with it. It belongs with the other timing guard, not in front of the audit.
# It needs no compatibility repo, so it runs before the delegation above.
perfguard:
	go test -tags perfguard -count=1 -run TestNoqaResolutionStaysLinear ./internal/rules

## build: compile the astl binary into bin/ with version metadata
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(MAIN_PACKAGE)

## check: build, lint examples/, and confirm astl reports exactly what it should
# The answer to "did this build work?", with no Python and no corpus: it
# asserts the findings and the exit code together, so a binary that runs but
# reports nothing cannot pass. Regenerate the expectation after an
# intentional change with: ./$(BUILD_DIR)/$(BINARY) $(EXAMPLES) > $(EXAMPLES)/expected.txt
check: build
	@echo "==> ./$(BUILD_DIR)/$(BINARY) $(EXAMPLES)"
	@out=$$(./$(BUILD_DIR)/$(BINARY) $(EXAMPLES)); code=$$?; \
	printf '%s\n' "$$out"; \
	if [ "$$code" != "2" ]; then \
		echo "FAIL: expected exit code 2 (violations found), got $$code" >&2; \
		exit 1; \
	fi; \
	if ! printf '%s\n' "$$out" | diff -u $(EXAMPLES)/expected.txt - >/dev/null; then \
		echo "FAIL: output differs from $(EXAMPLES)/expected.txt" >&2; \
		printf '%s\n' "$$out" | diff -u $(EXAMPLES)/expected.txt - >&2; \
		exit 1; \
	fi; \
	echo "OK: $$(printf '%s\n' "$$out" | wc -l | tr -d ' ') findings, exit code 2, output matches $(EXAMPLES)/expected.txt"

## ci: run the full local pipeline (tidy, audit, check, parity, bench)
ci: tidy audit check parity bench

## clean: remove build artifacts
clean:
	go clean
	rm -rf $(BUILD_DIR) coverage.out coverage.full.out

## cover: run tests with coverage and fail below COVER_MIN
# internal/yamlscan is scanner code vendored from gopkg.in/yaml.v3 (see its
# NOTICE) and is excluded from the gate: the ratchet measures this repo's own
# code, and the vendored scanner is exercised end to end by the yamllint and
# parity tests rather than line by line.
cover:
	go test -covermode=atomic -coverprofile=coverage.full.out $(PKG)
	@grep -v "internal/yamlscan/" coverage.full.out > coverage.out
	@go tool cover -func=coverage.out | awk '/^total:/ {print "coverage: " $$3}'
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v min="$(COVER_MIN)" 'BEGIN { if (t+0 < min+0) { printf "FAIL: coverage %.1f%% < %d%%\n", t, min; exit 1 } }'

## fuzz: run each fuzz target for FUZZTIME (default 60s)
# Go fuzzes one target per process, so these run in sequence rather than as one
# invocation. The seed corpora already run as ordinary tests on every `make
# test`; this target is for going past them, and for reproducing a crash the
# corpus in testdata/fuzz has recorded.
FUZZTIME ?= 60s
FUZZ_TARGETS = internal/rules:FuzzLintFile \
               internal/yamllint:FuzzLoadConfig \
               internal/parse:FuzzNoqaIndex
fuzz:
	@for t in $(FUZZ_TARGETS); do \
		pkg=$${t%%:*}; fn=$${t##*:}; \
		echo "==> $$fn ($$pkg) for $(FUZZTIME)"; \
		go test -run '^$$' -fuzz "^$$fn$$" -fuzztime $(FUZZTIME) ./$$pkg/ || exit 1; \
	done

## help: list available targets
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## parity: assert output compatibility against the frozen ansible-lint corpus
# The corpus and golden files live in a sibling repo because they carry
# ansible-lint test data (GPL-3.0-or-later); ansible-static-lint itself stays MIT. Absent
# sibling is a loud skip, not a failure, so the repo stays buildable standalone.
parity:
	$(call in_parity_repo,check,parity)

## release: cut and publish a release (derive version, changelog, tag, push)
release:
	@./scripts/release.sh

## test: run the test suite with the race detector
test:
	go test -race $(PKG)

## tidy: format sources and tidy go.mod
tidy:
	gofmt -w .
	go mod tidy

## tools: install pinned Go development tools
tools:
	@echo "Installing Go tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Tools installed in $(shell go env GOBIN || go env GOPATH)/bin"
