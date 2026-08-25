# SUPER CLI MARIO — build for every target we support.
#
#   make            native binary (./mario or .\mario.exe)
#   make release    cross-compile all platforms into dist/
#   make test       unit tests
#   make race       unit tests under the race detector
#   make cover      coverage summary per package
#   make vet / fmt / fmtcheck / clean / run / demo

BINARY    := mario
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST      := dist
WEBDIST   := $(DIST)/web
LDFLAGS   := -s -w

# Every release target: OS, ARCH, binary suffix.
TARGETS := linux/amd64   \
           linux/arm64   \
           darwin/amd64  \
           darwin/arm64  \
           windows/amd64

GOFLAGS := CGO_ENABLED=0

.PHONY: all build release test race cover vet fmt fmtcheck run demo clean web web-serve $(TARGETS)

all: build

build:
	$(GOFLAGS) go build -ldflags '$(LDFLAGS)' -o $(BINARY) .

# One target per GOOS/GOARCH pair; dist/mario-<os>-<arch>[.exe].
# POSIX sh only (no bashisms): GitHub runners use dash as /bin/sh.
$(TARGETS):
	@mkdir -p $(DIST)
	@ref=$@; os=$${ref%/*}; arch=$${ref#*/}; \
	suffix=""; if [ "$$os" = "windows" ]; then suffix=".exe"; fi; \
	echo "BUILD $(DIST)/$(BINARY)-$$os-$$arch$$suffix"; \
	$(GOFLAGS) GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' \
		-o $(DIST)/$(BINARY)-$$os-$$arch$$suffix .

release: $(TARGETS)
	@cd $(DIST) && sha256sum $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-windows-* > SHA256SUMS 2>/dev/null || \
		shasum -a 256 $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-windows-* > SHA256SUMS

# Static browser build (GitHub Pages ready): the game itself compiled to
# WASM, rendered client-side. All asset paths relative. The Supabase URL
# and publishable key are embedded (board.Default*) from env or .env so
# the leaderboard works in the browser, which has no environment.
SUPA_URL := $(or $(SUPABASE_URL),$(shell sed -n 's/^SUPABASE_URL=//p' .env web/supabase.env 2>/dev/null | head -n 1))
SUPA_KEY := $(or $(SUPABASE_KEY),$(shell sed -n 's/^SUPABASE_KEY=//p' .env web/supabase.env 2>/dev/null | head -n 1))
WEBLDFLAGS := $(LDFLAGS) -X mario/board.DefaultURL=$(SUPA_URL) -X mario/board.DefaultKey=$(SUPA_KEY)

web:
	@if [ -z "$(SUPA_URL)" ] || [ -z "$(SUPA_KEY)" ]; then \
		echo "ERROR: SUPABASE_URL and SUPABASE_KEY must be set in env, .env, or web/supabase.env" >&2; exit 1; \
	fi
	@mkdir -p $(DIST)/web
	GOOS=js GOARCH=wasm $(GOFLAGS) go build -ldflags "$(WEBLDFLAGS)" -o $(DIST)/web/mario.wasm .
	cp web/index.html $(WEBDIST)/
	@wasm_exec=$$(find "$$(go env GOROOT)" -name wasm_exec.js -type f | head -n 1); \
		[ -n "$$wasm_exec" ] || { echo "wasm_exec.js not found under GOROOT" >&2; exit 1; }; \
		cp "$$wasm_exec" $(WEBDIST)/ && chmod u+w $(WEBDIST)/wasm_exec.js
	@echo "static site in $(WEBDIST) — drop it on GitHub Pages (or any static host)"

web-serve: web
	@echo "serving $(WEBDIST) at http://127.0.0.1:8417/"
	@cd $(WEBDIST) && python3 -m http.server 8417 --bind 127.0.0.1

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmtcheck:
	@out=$$(gofmt -l .); [ -z "$$out" ] || (echo "gofmt needed:"; echo "$$out"; exit 1)

run: build
	./$(BINARY)

demo: build
	./$(BINARY) -demo

clean:
	rm -rf $(DIST) $(BINARY)
