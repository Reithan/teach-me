SHELL := /bin/sh

TOOLS_BIN    := .tools/bin
GOLANGCI     := $(TOOLS_BIN)/golangci-lint
GOLANGCI_VER := v2.13.2
DIFFCOVER_VER := 10.5.1

.PHONY: tools hooks precommit check lint test coverage diff-coverage fuzz vuln version-sync

## tools: install golangci-lint, diff-cover, and (if present) conformance deps.
tools:
	mkdir -p $(TOOLS_BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
		| sh -s -- -b $(TOOLS_BIN) $(GOLANGCI_VER)
	@command -v pipx >/dev/null 2>&1 || { \
		printf 'err: pipx not found\nfix: install pipx then re-run make tools\n' >&2; exit 1; }
	pipx install "diff-cover==$(DIFFCOVER_VER)" || pipx upgrade "diff-cover"
	@if [ -d conformance ]; then (cd conformance && npm ci); fi

## hooks: configure git to use .githooks/.
hooks:
	git config core.hooksPath .githooks

## lint: run golangci-lint.
lint:
	@[ -x $(GOLANGCI) ] || { printf 'err: golangci-lint not found\nfix: make tools\n' >&2; exit 1; }
	$(GOLANGCI) run ./...

## precommit: lint, build, tidy check, and version-sync.
precommit: lint
	go build ./...
	go mod tidy -diff
	$(MAKE) version-sync

## test: run tests with race detector.
test:
	go test -race ./...

## coverage: generate coverage profile excluding cmd/tm and internal/tools.
coverage:
	go test -race -covermode=atomic -coverprofile=coverage.out \
		$(shell go list ./... | grep -Ev 'github.com/reithan/teach-me/(cmd/tm|internal/tools)(/|$$)')
	go tool gocover-cobertura < coverage.out > coverage.xml
	sed -i 's|github.com/reithan/teach-me/||g' coverage.xml

## diff-coverage: statement coverage of lines changed on the branch vs origin/main.
diff-coverage: coverage
	@command -v diff-cover >/dev/null 2>&1 || { \
		printf 'err: diff-cover not found\nfix: make tools\n' >&2; exit 1; }
	diff-cover coverage.xml --compare-branch=origin/main --fail-under=85

## fuzz: run fuzz targets for 10s each (no-op if none exist).
fuzz:
	@targets=$$(grep -rh '^func Fuzz' --include='*.go' . | sed 's/func \(Fuzz[A-Za-z0-9_]*\).*/\1/' || true); \
	if [ -z "$$targets" ]; then \
		echo "fuzz: no fuzz targets found, skipping"; \
		exit 0; \
	fi; \
	for pkg in $$(go list ./...); do \
		for fn in $$(grep -h '^func Fuzz' "$$(go list -f '{{.Dir}}' $$pkg)"/*.go 2>/dev/null | sed 's/func \(Fuzz[A-Za-z0-9_]*\).*/\1/' || true); do \
			echo "fuzzing $$pkg -run=$$fn"; \
			go test -fuzz=$$fn -fuzztime=10s $$pkg || exit 1; \
		done; \
	done

## conformance: generate the corpus then run the Mermaid conformance suite.
conformance:
	@if [ ! -d conformance ]; then \
		echo "conformance: not present, skipping"; \
		exit 0; \
	fi
	go run ./internal/tools/corpus -out conformance/corpus
	node conformance/parse.mjs

## vuln: run govulncheck.
vuln:
	go tool govulncheck ./...

## version-sync: verify major.minor in VERSION matches metadata.tm-version in SKILL.md.
version-sync:
	@if [ ! -f skill/teach-me/SKILL.md ]; then \
		echo "version-sync: skill/teach-me/SKILL.md not found, skipping"; \
		exit 0; \
	fi
	@ver=$$(cat internal/version/VERSION | tr -d '[:space:]'); \
	major_minor=$$(echo "$$ver" | sed 's/[-+].*//; s/\.[0-9]*$$//'); \
	skill_ver=$$(grep 'tm-version:' skill/teach-me/SKILL.md | head -1 \
		| sed 's/.*tm-version:[[:space:]]*//' | tr -d '"'"'"' '); \
	if [ "$$major_minor" != "$$skill_ver" ]; then \
		printf 'err: version mismatch: VERSION=%s major.minor=%s, SKILL.md tm-version=%s\n' \
			"$$ver" "$$major_minor" "$$skill_ver" >&2; \
		exit 1; \
	fi; \
	echo "version-sync ok ($$major_minor)"

## check: full local CI — lint, test, diff-coverage, fuzz, conformance, vuln, version-sync.
check: lint test diff-coverage fuzz conformance vuln version-sync
