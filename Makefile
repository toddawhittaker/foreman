# Make targets are described in docs/STACK.md, section 13.
# Go runs locally; golangci-lint and Playwright run in pinned images.

# The one toolchain pin: local Go, the build image and CI all use it.
GO_VERSION      := 1.27.3
GO_IMAGE        := golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385
LINT_IMAGE      := golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481
PLAYWRIGHT_IMAGE := mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f
GOVULNCHECK     := golang.org/x/vuln/cmd/govulncheck@v1.9.0
COVERAGE_FLOOR  := 70

# Never let the go command fetch a different toolchain.
export GOTOOLCHAIN := local

IMAGE ?= foreman:dev

CACHE := $(CURDIR)/.cache
# Tool containers see the repository at its own path and keep caches in .cache/.
DOCKER_RUN := docker run --rm --init -v $(CURDIR):$(CURDIR) -w $(CURDIR) -u $(shell id -u):$(shell id -g) \
	-e HOME=$(CACHE)/home -e GOTOOLCHAIN=local \
	-e GOCACHE=$(CACHE)/go-build -e GOMODCACHE=$(CACHE)/gomod \
	-e GOLANGCI_LINT_CACHE=$(CACHE)/golangci -e npm_config_cache=$(CACHE)/npm

.PHONY: verify check-go fmt-check fmt vet lint test typecheck build image run e2e e2e-deps \
	test-game e2e-game vuln ci tidy

verify: check-go fmt-check vet lint test typecheck

# The local toolchain and the build image must be the pinned Go.
check-go:
	@have=$$(go env GOVERSION | sed 's/^go//'); \
	if [ "$$have" != "$(GO_VERSION)" ]; then \
		echo "Your local Go is $$have, but this project pins Go $(GO_VERSION). Install it (see docs/WORKFLOW.md, \"Local development\")."; \
		exit 1; \
	fi
	@case '$(GO_IMAGE)' in golang:$(GO_VERSION)-*) ;; *) \
		echo "GO_IMAGE in the Makefile is not a golang:$(GO_VERSION) image. Bring it in line with GO_VERSION."; exit 1;; esac

fmt-check:
	@out=$$(gofmt -l cmd internal); \
	if [ -n "$$out" ]; then echo "These files need gofmt (run make fmt):"; echo "$$out"; exit 1; fi

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

lint:
	@mkdir -p $(CACHE)/home
	$(DOCKER_RUN) $(LINT_IMAGE) golangci-lint run ./...

# One race-enabled run gives both the test result and the coverage floor.
test:
	go test -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out ./...
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ { sub("%", "", $$3); print $$3 }'); \
	if awk -v t="$$total" -v f=$(COVERAGE_FLOOR) 'BEGIN { exit !(t < f) }'; then \
		echo "Coverage across internal/ is $$total%, below the floor of $(COVERAGE_FLOOR)%."; exit 1; \
	fi; \
	echo "Coverage across internal/: $$total% (floor $(COVERAGE_FLOOR)%)."

e2e/node_modules/.package-lock.json: e2e/package.json e2e/package-lock.json
	@mkdir -p $(CACHE)/home
	$(DOCKER_RUN) -w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) npm ci --no-audit --no-fund
	@touch $@

e2e-deps: e2e/node_modules/.package-lock.json

typecheck: e2e-deps
	$(DOCKER_RUN) -w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) npx --no-install tsc --noEmit

build:
	CGO_ENABLED=0 go build -trimpath -o bin/foreman ./cmd/foreman

image:
	docker build --build-arg GO_IMAGE=$(GO_IMAGE) -t $(IMAGE) .

run:
	@mkdir -p .dev-data
	FOREMAN_MODE=managed FOREMAN_GAME_UID=$(shell id -u) FOREMAN_GAME_GID=$(shell id -g) \
		FOREMAN_DATA_DIR=$(CURDIR)/.dev-data FOREMAN_HOST_DATA_DIR=$(CURDIR)/.dev-data \
		go run ./cmd/foreman serve

e2e: build e2e-deps
	$(DOCKER_RUN) --ipc=host -w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) npx --no-install playwright test

test-game:
	@echo "make test-game arrives in Epic 3, with server control."

e2e-game:
	@echo "make e2e-game arrives in Epic 3, with server control."

vuln:
	go run $(GOVULNCHECK) ./...

# Everything CI checks, in one local run.
ci: verify e2e image

tidy:
	go mod tidy
