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

.PHONY: verify check-go fmt-check fmt vet lint test test-shards typecheck build build-fakegame build-fakeportal image run e2e e2e-deps \
	test-game test-game-resources game-images e2e-game vuln ci tidy i18n-hash deploy

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

# The web package takes about 11 minutes under -race on a loaded machine,
# past go test's 10-minute default, so the limit is set here with room.
TEST_TIMEOUT ?= 30m

# Local runs draw batches of tests from a queue (POOL_PKGS below) with this
# many workers, each a process, at once. The default, auto, is one per CPU,
# held down so the processes fit in the memory left (1.5 GiB each, the race
# detector's cost); see RESOLVE_SHARDS below. A number sets the count
# outright. Under CI the default is one process, the plain `go test ./...`
# run; the CI workflow passes TEST_SHARDS=auto to use the pool.
TEST_SHARDS ?= $(if $(CI),1,auto)

# The slow packages whose tests make up the local queue. The order sets who
# goes first in each round of the queue.
POOL_PKGS := ./internal/game ./internal/world ./internal/web

# How many tests one queued batch holds. Each batch is a new process, so
# small batches balance the workers better but pay the start-up cost (the
# race detector and the coverage counters) more often; large ones the
# reverse.
TEST_BATCH ?= 8

# One queued batch, run by `sh -c` with the fields of a queue line as
# arguments: the package folder, the test binary, the batch name and the
# anchored -test.run pattern. The caller sets POOL_WORK to the work folder.
# The output, and the exit code and seconds, are written there for the report.
define POOL_BATCH
dir=$$1; bin=$$2; id=$$3; run=$$4; t0=$$(date +%s); mkdir "$$POOL_WORK/cov/$$id"; \
( cd "$$dir" && "$$bin" -test.run "$$run" -test.timeout $(TEST_TIMEOUT) -test.gocoverdir="$$POOL_WORK/cov/$$id" ) > "$$POOL_WORK/out/$$id" 2>&1; rc=$$?; \
echo "$$rc $$(( $$(date +%s) - t0 ))" > "$$POOL_WORK/rc/$$id"
endef

# Sets n (the process count) and msg (the count and why) in the shell. For
# a number it checks the number. For auto it counts CPUs, then takes the
# smallest memory headroom it can find: MemAvailable, and for every cgroup v2
# level from this process's own up to the root, the limit (memory.high, or
# memory.max when that is max) minus memory.current. Agents on a shared
# machine run under such a limit, and past memory.high the kernel throttles
# allocations instead of failing them, which is far slower than fewer
# processes. awk does the arithmetic because the values pass 32 bits. With no
# headroom figure at all (not Linux, no cgroup v2), n is the CPU count.
define RESOLVE_SHARDS
n=$(TEST_SHARDS); \
case "$$n" in \
auto) \
	cpus=$$(getconf _NPROCESSORS_ONLN 2>/dev/null); \
	case "$$cpus" in ''|*[!0-9]*|0) cpus=1;; esac; \
	ma=$$(awk '/^MemAvailable:/ { printf "%.0f", $$2 * 1024 }' /proc/meminfo 2>/dev/null); \
	pairs=""; \
	d="/sys/fs/cgroup$$(sed -n 's/^0:://p' /proc/self/cgroup 2>/dev/null)"; d="$${d%/}"; \
	while :; do \
		cur=$$(cat "$$d/memory.current" 2>/dev/null); \
		lim=$$(cat "$$d/memory.high" 2>/dev/null); \
		case "$$lim" in ''|max) lim=$$(cat "$$d/memory.max" 2>/dev/null);; esac; \
		case "$$cur$$lim" in ''|*[!0-9]*) ;; *) pairs="$$pairs $$lim:$$cur";; esac; \
		case "$$d" in /sys/fs/cgroup/*) d="$${d%/*}";; *) break;; esac; \
	done; \
	res=$$(awk -v cpus="$$cpus" -v ma="$$ma" -v pairs="$$pairs" 'BEGIN { have = 0; if (ma != "") { have = 1; m = ma + 0 }; k = split(pairs, p, " "); for (i = 1; i <= k; i++) { split(p[i], q, ":"); h = q[1] - q[2]; if (h < 0) h = 0; if (!have || h < m) m = h; have = 1 }; if (!have) { print cpus, "-"; exit }; c = int(m / 1610612736); if (c > cpus) c = cpus; if (c < 1) c = 1; printf "%d %.1f\n", c, m / 1073741824 }'); \
	n=$${res%% *}; gib=$${res#* }; \
	cpuword=CPUs; if [ "$$cpus" -eq 1 ]; then cpuword=CPU; fi; \
	if [ "$$gib" = "-" ]; then why="$$cpus $$cpuword, memory headroom unknown"; else why="$$cpus $$cpuword, $$gib GiB memory headroom"; fi;; \
*) \
	case "$$n" in ''|*[!0-9]*|0) echo "TEST_SHARDS must be a positive whole number or auto."; exit 1;; esac; \
	why="$(if $(filter file,$(origin TEST_SHARDS)),CI,set by TEST_SHARDS)";; \
esac; \
word=processes; if [ "$$n" -eq 1 ]; then word=process; fi; \
msg="$$n $$word ($$why)"
endef

# Shows how many processes `make test` would use, and why, without testing.
test-shards:
	@$(RESOLVE_SHARDS); echo "$$msg."

# One race-enabled run gives both the test result and the coverage floor.
ifeq ($(TEST_SHARDS),1)
test:
	@cov=$$(mktemp) && trap 'rm -f "$$cov"' EXIT && \
	go test -race -timeout $(TEST_TIMEOUT) -covermode=atomic -coverpkg=./internal/... -coverprofile="$$cov" ./... || exit $$?; \
	total=$$(go tool cover -func="$$cov" | awk '/^total:/ { sub("%", "", $$3); print $$3 }'); \
	if awk -v t="$$total" -v f=$(COVERAGE_FLOOR) 'BEGIN { exit !(t < f) }'; then \
		echo "Coverage across internal/ is $$total%, below the floor of $(COVERAGE_FLOOR)%."; exit 1; \
	fi; \
	echo "Coverage across internal/: $$total% (floor $(COVERAGE_FLOOR)%)."
else
# With more than one worker, the slow packages' test binaries are built once
# and their tests become a queue of small batches, which TEST_SHARDS workers
# draw from one at a time. Each batch is its own process. The other packages
# run beside the pool. Each batch writes coverage to its own folder, and the
# folders are merged before the floor check.
test:
	@$(RESOLVE_SHARDS); \
	work=$$(mktemp -d) && trap 'rm -rf "$$work"' EXIT && \
	mkdir "$$work/bin" "$$work/lists" "$$work/cov" "$$work/cov-other" "$$work/out" "$$work/rc" && \
	go test -c -race -covermode=atomic -coverpkg=./internal/... -o "$$work/bin/" $(POOL_PKGS) || exit $$?; \
	for p in $(POOL_PKGS); do \
		base=$${p##*/}; \
		(cd "$$p" && "$$work/bin/$$base.test" -test.list '.*' 2>/dev/null) | grep -v '^Benchmark' > "$$work/lists/$$base"; \
		if [ ! -s "$$work/lists/$$base" ]; then echo "Found no tests in $${p#./}." >&2; exit 1; fi; \
		awk -v size=$(TEST_BATCH) '{ r = r (r == "" ? "" : "|") $$0 } NR % size == 0 { print r; r = "" } END { if (r != "") print r }' "$$work/lists/$$base" | \
		{ i=0; while read -r run; do \
			i=$$((i + 1)); \
			printf '%s %s %s %s-%03d %s\n' "$$i" "$${p#./}" "$$work/bin/$$base.test" "$$base" "$$i" "^($$run)\$$"; \
		done; }; \
	done > "$$work/queue.in" || exit $$?; \
	sort -s -n -k1,1 "$$work/queue.in" | cut -d' ' -f2- > "$$work/queue"; \
	batches=$$(wc -l < "$$work/queue"); \
	if [ "$$n" -gt "$$batches" ]; then n=$$batches; fi; \
	word=workers; if [ "$$n" -eq 1 ]; then word=worker; fi; \
	names=$$(echo $(patsubst ./%,%,$(POOL_PKGS)) | awk '{ s = $$1; for (i = 2; i < NF; i++) s = s ", " $$i; if (NF > 1) s = s " and " $$NF; print s }'); \
	echo "Running $$names as $$batches batches on $$n $$word ($$why)."; \
	go list $(POOL_PKGS) > "$$work/pool.pkgs"; \
	pkgs=$$(go list ./... | grep -vxFf "$$work/pool.pkgs"); \
	( go test -race -count=1 -timeout $(TEST_TIMEOUT) -covermode=atomic -coverpkg=./internal/... $$pkgs -args -test.gocoverdir="$$work/cov-other" > "$$work/other.out" 2>&1; echo $$? > "$$work/other.rc" ) & \
	POOL_WORK="$$work" xargs -P "$$n" -n 4 sh -c '$(POOL_BATCH)' sh < "$$work/queue"; \
	wait; \
	failed=0; \
	for p in $(POOL_PKGS); do \
		base=$${p##*/}; bad=0; count=0; slow=0; \
		while read -r dir bin id run; do \
			case "$$id" in "$$base"-*) ;; *) continue;; esac; \
			count=$$((count + 1)); rc=1; secs=0; \
			read rc secs < "$$work/rc/$$id" || true; \
			if [ "$$rc" -ne 0 ]; then bad=$$((bad + 1)); echo "$$id" >> "$$work/failed"; fi; \
			if [ "$$secs" -gt "$$slow" ]; then slow=$$secs; fi; \
		done < "$$work/queue"; \
		tests=$$(wc -l < "$$work/lists/$$base"); \
		if [ "$$bad" -eq 0 ]; then \
			printf 'ok    %s  %s tests in %s batches  slowest batch %ss\n' "$${p#./}" "$$tests" "$$count" "$$slow"; \
		else \
			printf 'FAIL  %s  %s tests in %s batches, %s failed  slowest batch %ss\n' "$${p#./}" "$$tests" "$$count" "$$bad" "$$slow"; failed=1; \
		fi; \
	done; \
	cat "$$work/other.out"; \
	read rc < "$$work/other.rc" || rc=1; \
	if [ "$$rc" -ne 0 ]; then failed=1; fi; \
	if [ -s "$$work/failed" ]; then \
		while read -r id; do echo "Output of failed batch $$id:"; cat "$$work/out/$$id"; done < "$$work/failed"; \
	fi; \
	if [ "$$failed" -ne 0 ]; then exit 1; fi; \
	cov="$$work/cover.out"; \
	dirs=$$(ls -d "$$work"/cov/* "$$work/cov-other" | paste -sd, -); \
	go tool covdata textfmt -i="$$dirs" -o "$$cov" || exit $$?; \
	total=$$(go tool cover -func="$$cov" | awk '/^total:/ { sub("%", "", $$3); print $$3 }'); \
	if awk -v t="$$total" -v f=$(COVERAGE_FLOOR) 'BEGIN { exit !(t < f) }'; then \
		echo "Coverage across internal/ is $$total%, below the floor of $(COVERAGE_FLOOR)%."; exit 1; \
	fi; \
	echo "Coverage across internal/: $$total% (floor $(COVERAGE_FLOOR)%)."
endif

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

# The fake mod portal for the end-to-end tests, likewise never in the image.
build-fakeportal:
	CGO_ENABLED=0 go build -trimpath -o bin/fakeportal ./cmd/fakeportal

image:
	docker build --build-arg GO_IMAGE=$(GO_IMAGE) -t $(IMAGE) .

run:
	@mkdir -p .dev-data
	FOREMAN_GAME_UID=$(shell id -u) FOREMAN_GAME_GID=$(shell id -g) \
		FOREMAN_DATA_DIR=$(CURDIR)/.dev-data FOREMAN_HOST_DATA_DIR=$(CURDIR)/.dev-data \
		go run ./cmd/foreman serve

# SHARD=1/8 runs the first of eight slices of the tests (CI uses eight); empty
# runs them all.
SHARD ?=
# E2E_ARGS passes specs or flags to Playwright, e.g. E2E_ARGS="tests/tips.spec.ts".
E2E_ARGS ?=

e2e: build build-fakegame build-fakeportal e2e-deps
	$(DOCKER_RUN) --ipc=host -e CI -w $(CURDIR)/e2e $(PLAYWRIGHT_IMAGE) npx --no-install playwright test $(if $(SHARD),--shard=$(SHARD)) $(E2E_ARGS)

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
# after another too). TAG=1.1, 2.0 or 2.1 runs one line. The real-game tests
# cover the latest patch of each line only.
# The images each TAG's tests run: TAG=2.0 and 2.1 include the 2.0.77 to
# 2.1.24 update.
TEST_GAME_USES_ := 1.1 2.0 2.1
TEST_GAME_USES_1.1 := 1.1
TEST_GAME_USES_2.0 := 2.0 2.1
TEST_GAME_USES_2.1 := 2.0 2.1

GAME_GO_TEST = GAME_IMAGE_1_1=$(GAME_IMAGE_1_1) GAME_IMAGE_2_0=$(GAME_IMAGE_2_0) GAME_IMAGE_2_1=$(GAME_IMAGE_2_1) \
	FOREMAN_TEST_GAME_TAG=$(TAG) go test -tags game -count=1 -p 1 -v

# The resource count's tests take most of a run, so they have their own
# target and time limit; test-game runs them after the rest.
GAME_RESOURCE_TESTS := ^TestRealGameD2Resource
GAME_RESOURCES_RUN = $(GAME_GO_TEST) -timeout 90m -run '$(GAME_RESOURCE_TESTS)' ./internal/game/

# Checks TAG and that every image the run needs is on the host, before any
# test starts.
game-images:
	@case "$(TAG)" in ""|1.1|2.0|2.1) ;; *) echo "TAG must be 1.1, 2.0 or 2.1."; exit 1;; esac
	$(if $(filter 1.1,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_1_1)))
	$(if $(filter 2.0,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_0)))
	$(if $(filter 2.1,$(TEST_GAME_USES_$(TAG))),$(call CHECK_GAME_IMAGE,$(GAME_IMAGE_2_1)))

# The resource tests run even when the rest fail, and the first failure's
# status is the target's.
test-game: game-images
	status=0; \
	$(GAME_GO_TEST) -timeout 30m -run TestRealGame -skip '$(GAME_RESOURCE_TESTS)' ./internal/game/ ./internal/web/ || status=$$?; \
	$(GAME_RESOURCES_RUN) || second=$$?; \
	[ $$status -ne 0 ] || status=$${second:-0}; \
	exit $$status

test-game-resources: game-images
	$(GAME_RESOURCES_RUN)

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
