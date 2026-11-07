# Make targets are described in docs/STACK.md, section 13.
# Go runs locally; golangci-lint and Playwright run in pinned images.

# Your own values (DEPLOY_DIR) live in local.mk, which git ignores.
-include local.mk

# The one toolchain pin: local Go, the build image and CI all use it.
GO_VERSION      := 1.27.3
GO_IMAGE        := golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385
LINT_IMAGE      := golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481
PLAYWRIGHT_IMAGE := mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f
GAME_IMAGE_1_1  := factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb
GAME_IMAGE_2_0  := factoriotools/factorio:2.0.77@sha256:312341f29987b5a6b11f00ba8240192f7564936af9913384d13f7f4c535708ef
GAME_IMAGE_2_1  := factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28
GAME_IMAGE_2_1_PREV := factoriotools/factorio:2.1.23@sha256:1e1773337f5be1d93f61d1d64757f18c19c8eb345c96acc57e0d3567a4fd4c3f
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

.PHONY: verify check-go fmt-check fmt vet lint test typecheck build build-fakegame image run e2e e2e-deps \
	test-game e2e-game vuln ci tidy i18n-hash deploy

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
	@cov=$$(mktemp) && trap 'rm -f "$$cov"' EXIT && \
	go test -race -covermode=atomic -coverpkg=./internal/... -coverprofile="$$cov" ./... || exit $$?; \
	total=$$(go tool cover -func="$$cov" | awk '/^total:/ { sub("%", "", $$3); print $$3 }'); \
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

# The fake game for the end-to-end tests. It is never copied into the image.
build-fakegame:
	CGO_ENABLED=0 go build -trimpath -o bin/fakegame ./cmd/fakegame

image:
	docker build --build-arg GO_IMAGE=$(GO_IMAGE) -t $(IMAGE) .

run:
	@mkdir -p .dev-data
	FOREMAN_GAME_UID=$(shell id -u) FOREMAN_GAME_GID=$(shell id -g) \
		FOREMAN_DATA_DIR=$(CURDIR)/.dev-data FOREMAN_HOST_DATA_DIR=$(CURDIR)/.dev-data \
		go run ./cmd/foreman serve

# SHARD=1/4 runs the first of four slices of the tests; empty runs them all.
SHARD ?=
# E2E_ARGS passes specs or flags to Playwright, e.g. E2E_ARGS="tests/tips.spec.ts".
E2E_ARGS ?=

e2e: build build-fakegame e2e-deps
	$(DOCKER_RUN) --ipc=host -w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) npx --no-install playwright test $(if $(SHARD),--shard=$(SHARD)) $(E2E_ARGS)

# Foreman runs a world on the plain tag factoriotools/factorio:<version>, so
# the tests do too. A tag can move or be re-pushed, so before a real-game run
# this checks that the host's tag is the image the Makefile pins, by digest.
define CHECK_GAME_IMAGE
	@pinned='$(1)'; tag=$${pinned%@*}; digest=$${pinned#*@}; \
	have=$$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$$tag" 2>/dev/null); \
	case "$$have" in *"@$$digest"*) ;; *) \
		echo "The image $$tag on this host is missing or is not the pinned $$digest."; \
		echo "Fix it with: docker pull $$pinned && docker tag $${tag%:*}@$$digest $$tag"; exit 1;; esac
endef

# The controller against the real Docker daemon and the pinned game images,
# one image at a time (they share the game port, so -p 1 runs the packages one
# after another too). TAG=1.1, 2.0 or 2.1 runs one.
# The images each TAG's tests run: TAG=2.0 and 2.1 include the 2.0.77 to
# 2.1.24 update, and 2.1 the 2.1.23 to 2.1.24 one too.
TEST_GAME_USES_ := 1.1 2.0 2.1 2.1-prev
TEST_GAME_USES_1.1 := 1.1
TEST_GAME_USES_2.0 := 2.0 2.1
TEST_GAME_USES_2.1 := 2.0 2.1 2.1-prev

test-game:
	@case "$(TAG)" in ""|1.1|2.0|2.1) ;; *) echo "TAG must be 1.1, 2.0 or 2.1."; exit 1;; esac
	$(if $(filter 1.1,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_1_1)))
	$(if $(filter 2.0,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_0)))
	$(if $(filter 2.1,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_1)))
	$(if $(filter 2.1-prev,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_1_PREV)))
	GAME_IMAGE_1_1=$(GAME_IMAGE_1_1) GAME_IMAGE_2_0=$(GAME_IMAGE_2_0) GAME_IMAGE_2_1=$(GAME_IMAGE_2_1) GAME_IMAGE_2_1_PREV=$(GAME_IMAGE_2_1_PREV) \
		FOREMAN_TEST_GAME_TAG=$(TAG) go test -tags game -count=1 -p 1 -timeout 30m -v -run TestRealGame ./internal/game/ ./internal/web/

# The phone scenario against the real game: Playwright in its pinned image,
# with the host's Docker socket, starts bin/foreman, which starts the pinned
# 2.1 image on a network of this run's own: SPEC 4.4 refuses a game network
# that other containers share, as the default bridge may. The repository is mounted at its own path,
# so the bind paths Foreman gives Docker are the host's. The game is a sibling
# container that Foreman leaves running, so this target removes it, even on failure.
DOCKER_SOCK ?= /var/run/docker.sock

e2e-game: build e2e-deps
	$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_1))
	@test -S $(DOCKER_SOCK) || { echo "$(DOCKER_SOCK) is not a Docker socket. Set DOCKER_SOCK to the host's."; exit 1; }
	@mkdir -p $(CACHE)/home $(CACHE)/e2e-game
	@net=foreman-e3game-net-$$$$; \
	clean() { \
		docker ps -aq --filter name=foreman-e3game- | xargs -r docker rm -f -v >/dev/null; \
		docker network rm $$net >/dev/null 2>&1; \
		rm -rf $(CACHE)/e2e-game; }; \
	trap clean EXIT; trap 'exit 130' INT TERM; \
	docker network create $$net >/dev/null && \
	$(DOCKER_RUN) --network $$net -e FOREMAN_E2E_GAME_NETWORK=$$net --ipc=host -v $(DOCKER_SOCK):/var/run/docker.sock --group-add $$(stat -c %g $(DOCKER_SOCK)) \
		-w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) sh -c 'npx --no-install tsc --noEmit -p game && npx --no-install playwright test -c game'

# Build the image from this checkout and recreate only Foreman in a local
# Compose stack, so the proxy, the sign-in outpost and a running game stay up.
# DEPLOY_DIR has no default: the repository names no deployment.
DEPLOY_IMAGE ?= $(IMAGE)
DEPLOY_SERVICE ?= foreman
DEPLOY_TIMEOUT ?= 120
FORCE ?=

deploy:
	@test -n '$(DEPLOY_DIR)' || { \
		echo "DEPLOY_DIR is not set. Set it to the folder of your Compose stack, on the command line or in local.mk"; \
		echo "(docs/WORKFLOW.md, \"Local development\"). Example: make deploy DEPLOY_DIR=/path/to/stack"; exit 1; }
	@test -d '$(DEPLOY_DIR)' || { echo "DEPLOY_DIR '$(DEPLOY_DIR)' is not a folder."; exit 1; }
	@if [ -z '$(FORCE)' ] && [ -n "$$(git status --porcelain)" ]; then \
		echo "This checkout has uncommitted changes, so what runs would match no commit."; \
		echo "Commit them, or run make deploy FORCE=1 to deploy anyway."; exit 1; fi
	$(MAKE) image IMAGE='$(DEPLOY_IMAGE)'
	docker compose --project-directory '$(DEPLOY_DIR)' up -d --no-deps '$(DEPLOY_SERVICE)'
	@cid=$$(docker compose --project-directory '$(DEPLOY_DIR)' ps -q '$(DEPLOY_SERVICE)'); \
	[ -n "$$cid" ] || { echo "The service '$(DEPLOY_SERVICE)' has no container in '$(DEPLOY_DIR)'."; exit 1; }; \
	echo "Waiting up to $(DEPLOY_TIMEOUT) s for $(DEPLOY_SERVICE) to report healthy."; \
	waited=0; \
	while :; do \
		state=$$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$$cid"); \
		[ "$$state" = healthy ] && break; \
		if [ "$$state" = unhealthy ] || [ "$$state" = no-healthcheck ] || [ "$$waited" -ge '$(DEPLOY_TIMEOUT)' ]; then \
			echo "$(DEPLOY_SERVICE) is $$state after $$waited s, so the deploy failed. See: docker logs $$cid"; exit 1; fi; \
		sleep 2; waited=$$((waited + 2)); \
	done; \
	echo "Deployed $(DEPLOY_SERVICE): healthy after $$waited s."; \
	echo "Image ID: $$(docker inspect -f '{{.Image}}' "$$cid")"; \
	echo "Commit:   $$(git rev-parse HEAD)$(if $(FORCE), (FORCE=1: the tree may differ from this commit))"

vuln:
	go run $(GOVULNCHECK) ./...

# Everything CI checks, in one local run.
ci: verify e2e image

tidy:
	go mod tidy

# After updating every language for a key, record the English it was
# translated from (internal/i18n/translated/<area>.json, SPEC 22.5).
i18n-hash:
	go run ./cmd/i18n-hash
