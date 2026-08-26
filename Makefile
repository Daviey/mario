# SUPER CLI MARIO — build for every target we support.
#
#   make            native binary (./mario or .\mario.exe)
#   make release    cross-compile all platforms into dist/ (parallel)
#   make check      fmtcheck + vet + test (the CI pre-release gate)
#   make race       unit tests under the race detector
#   make cover      coverage summary per package
#   make vet / fmt / fmtcheck / clean / run / demo

BINARY    := mario
# VERSION feeds shell recipes (ldflags quoting, sed, mkdeb args), so strip
# everything outside a safe charset at this single choke point — a crafted
# git tag name must never reach recipe-shell evaluation.
VERSION   := $(shell v=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); printf '%s' "$$v" | tr -cd '[:alnum:]_.+-')
DIST      := dist
WEBDIST   := $(DIST)/web
LDFLAGS   := -s -w -X github.com/Daviey/mario/render.Version=$(VERSION)

# Every release target: OS, ARCH, binary suffix.
TARGETS := linux/amd64   \
           linux/arm64   \
           darwin/amd64  \
           darwin/arm64  \
           windows/amd64

GOFLAGS := CGO_ENABLED=0

.PHONY: all build release check test race cover vet fmt fmtcheck run demo clean web web-serve deb $(TARGETS) deb/amd64 deb/arm64

all: build

build:
	$(GOFLAGS) go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/mario

# One target per GOOS/GOARCH pair; dist/mario-<os>-<arch>[.exe].
# POSIX sh only (no bashisms): GitHub runners use dash as /bin/sh.
$(TARGETS):
	@mkdir -p $(DIST)
	@ref=$@; os=$${ref%/*}; arch=$${ref#*/}; \
	suffix=""; if [ "$$os" = "windows" ]; then suffix=".exe"; fi; \
	echo "BUILD $(DIST)/$(BINARY)-$$os-$$arch$$suffix"; \
	$(GOFLAGS) GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' \
		-o $(DIST)/$(BINARY)-$$os-$$arch$$suffix ./cmd/mario

# Parallel across targets (each go build parallelises internally, so -j is
# capped at one job per target).
release:
	+$(MAKE) -j$(words $(TARGETS)) $(TARGETS)
	@cd $(DIST) && sha256sum $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-windows-* > SHA256SUMS 2>/dev/null || \
		shasum -a 256 $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-windows-* > SHA256SUMS

# Debian packages via the pure-Go packager (tools/mkdeb) — no dpkg on the
# build host (the CI runner is NixOS). Installs /usr/games/mario + man6 +
# hicolor icon + desktop entry. PKGVERSION strips git describe's leading v.
PKGVERSION := $(patsubst v%,%,$(VERSION))

deb/amd64: linux/amd64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkdeb -version $(VERSION) -arch amd64 \
		-bin $(DIST)/$(BINARY)-linux-amd64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_amd64.deb

deb/arm64: linux/arm64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkdeb -version $(VERSION) -arch arm64 \
		-bin $(DIST)/$(BINARY)-linux-arm64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_arm64.deb

deb: deb/amd64 deb/arm64

# Static browser build (GitHub Pages ready): the game itself compiled to
# WASM, rendered client-side. All asset paths relative. The Supabase URL
# and publishable key are embedded (board.Default*) from env or .env so
# the leaderboard works in the browser, which has no environment.
SUPA_URL := $(or $(SUPABASE_URL),$(shell sed -n 's/^SUPABASE_URL=//p' .env web/supabase.env 2>/dev/null | head -n 1))
SUPA_KEY := $(or $(SUPABASE_KEY),$(shell sed -n 's/^SUPABASE_KEY=//p' .env web/supabase.env 2>/dev/null | head -n 1))
web:
	@if [ -z "$(SUPA_URL)" ] || [ -z "$(SUPA_KEY)" ]; then \
		echo "ERROR: SUPABASE_URL and SUPABASE_KEY must be set in env, .env, or web/supabase.env" >&2; exit 1; \
	fi
	@case "$(SUPA_KEY)" in \
		sb_publishable_*) ;; \
		eyJ*) \
			payload=$$(printf '%s' "$(SUPA_KEY)" | cut -d. -f2 | tr '_-' '/+'); \
			case $$(( $${#payload} % 4 )) in \
				2) payload="$$payload==" ;; \
				3) payload="$$payload=" ;; \
			esac; \
			printf '%s' "$$payload" | base64 -d 2>/dev/null | grep -q '"role":"anon"' || { \
				echo "ERROR: SUPABASE_KEY is a JWT without role=anon — refusing to embed a privileged key in a public artifact" >&2; exit 1; } ;; \
		*) echo "ERROR: SUPABASE_KEY is not publishable (want sb_publishable_* or an anon JWT)" >&2; exit 1 ;; \
	esac
	@mkdir -p $(DIST)/web
	cp web/boot.js $(WEBDIST)/
	GOOS=js GOARCH=wasm $(GOFLAGS) go build -ldflags "$(WEBLDFLAGS)" -o $(DIST)/web/mario.wasm ./cmd/web
	cp web/manifest.webmanifest $(WEBDIST)/
	# Narrow the CSP to exactly this project's origin (the source keeps the
	# dev-friendly https://*.supabase.co wildcard; only dist narrows).
	@supa_origin=$$(printf '%s' "$(SUPA_URL)" | sed -e 's|^[a-zA-Z]*://||' -e 's|/.*||'); \
		sed "s|https://\*.supabase.co|https://$$supa_origin|" web/index.html > $(WEBDIST)/index.html
	# Cache-bust the service worker per release: the deployed sw.js carries
	# the git version as its CACHE name, so every deploy keys a fresh cache
	# (activate drops older ones) instead of serving stale bytes forever.
	sed 's/mario-v0\.2\.0/mario-$(VERSION)/' web/sw.js > $(WEBDIST)/sw.js
	cp -r web/icons $(WEBDIST)/
	@wasm_exec=$$(find "$$(go env GOROOT)" -name wasm_exec.js -type f | head -n 1); \
		[ -n "$$wasm_exec" ] || { echo "wasm_exec.js not found under GOROOT" >&2; exit 1; }; \
		cp "$$wasm_exec" $(WEBDIST)/ && chmod u+w $(WEBDIST)/wasm_exec.js
	@echo "static site in $(WEBDIST) — drop it on GitHub Pages (or any static host)"

web-serve: web
	@echo "serving $(WEBDIST) at http://127.0.0.1:8417/"
	@cd $(WEBDIST) && python3 -m http.server 8417 --bind 127.0.0.1

# CI pre-release gate, in fail-fast order: formatting, vet, then tests.
check: fmtcheck vet test

test:
	$(GOFLAGS) go test ./...

race:
	go test -race ./...

cover:
	$(GOFLAGS) go test -cover ./...

# Static like test/build: the CI runner image has no C toolchain, and
# default-CGO vet would try to compile runtime/cgo (net/http resolver).
vet:
	$(GOFLAGS) go vet ./...

# fmt/fmtcheck touch tracked files only — mirrors what CI checks out, and
# keeps both from rewriting (or tripping on) WIP sources inside nested
# .worktrees/ checkouts.
fmt:
	git ls-files -- '*.go' | xargs gofmt -w

fmtcheck:
	@out=$$(git ls-files -- '*.go' | xargs gofmt -l); [ -z "$$out" ] || (echo "gofmt needed:"; echo "$$out"; exit 1)

run: build
	./$(BINARY)

demo: build
	./$(BINARY) -demo

clean:
	rm -rf $(DIST) $(BINARY)
