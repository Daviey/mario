# SUPER CLI MARIO — build for every target we support.
#
#   make            native binary (./mario or .\mario.exe)
#   make release    cross-compile all platforms into dist/ (parallel)
#   make check      fmtcheck + vet + test (the CI pre-release gate)
#   make race       unit tests under the race detector
#   make cover      coverage summary per package
#   make apk        Android APK (WebView shell around the web build)
#   make ipa        unsigned iOS .ipa (WKWebView shell; Linux clang+lld build)
#   make app        macOS .app bundle + universal darwin binary (tools/mkapp)
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
           linux/riscv64 \
           linux/arm     \
           linux/386     \
           darwin/amd64  \
           darwin/arm64  \
           freebsd/amd64 \
           freebsd/arm64 \
           openbsd/amd64 \
           openbsd/arm64 \
           netbsd/amd64  \
           netbsd/arm64  \
           solaris/amd64 \
           windows/amd64 \
           windows/arm64 \
           windows/386

GOFLAGS := CGO_ENABLED=0

.PHONY: all build release check test race cover vet fmt fmtcheck run demo clean web web-serve deb rpm apk ipa app shots $(TARGETS) deb/amd64 deb/arm64 deb/riscv64 deb/armhf deb/i386 rpm/amd64 rpm/arm64 rpm/riscv64 rpm/arm rpm/386 efi efi-initrd efi-qemu efi-qemu-ovmf

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
	@cd $(DIST) && sha256sum $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-freebsd-* $(BINARY)-openbsd-* $(BINARY)-netbsd-* $(BINARY)-solaris-* $(BINARY)-windows-* > SHA256SUMS 2>/dev/null || \
		shasum -a 256 $(BINARY)-linux-* $(BINARY)-darwin-* $(BINARY)-freebsd-* $(BINARY)-openbsd-* $(BINARY)-netbsd-* $(BINARY)-solaris-* $(BINARY)-windows-* > SHA256SUMS

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

deb/riscv64: linux/riscv64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkdeb -version $(VERSION) -arch riscv64 \
		-bin $(DIST)/$(BINARY)-linux-riscv64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_riscv64.deb

deb/armhf: linux/arm
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkdeb -version $(VERSION) -arch armhf \
		-bin $(DIST)/$(BINARY)-linux-arm -out $(DIST)/$(BINARY)_$(PKGVERSION)_armhf.deb

deb/i386: linux/386
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkdeb -version $(VERSION) -arch i386 \
		-bin $(DIST)/$(BINARY)-linux-386 -out $(DIST)/$(BINARY)_$(PKGVERSION)_i386.deb

deb: deb/amd64 deb/arm64 deb/riscv64 deb/armhf deb/i386

# RPM packages via the pure-Go packager (tools/mkrpm) — same contract as
# mkdeb, no rpm toolchain on the build host. Installs /usr/bin/mario
# (Fedora has no /usr/games convention) + man6 + hicolor icon +
# desktop entry. Output names use the RPM arch (x86_64, …) so they never
# match the dist/mario-* binary globs. Targets take the GOARCH name;
# the tool maps to the RPM architecture internally.
rpm/amd64: linux/amd64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkrpm -version $(VERSION) -arch amd64 \
		-bin $(DIST)/$(BINARY)-linux-amd64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_x86_64.rpm

rpm/arm64: linux/arm64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkrpm -version $(VERSION) -arch arm64 \
		-bin $(DIST)/$(BINARY)-linux-arm64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_aarch64.rpm

rpm/riscv64: linux/riscv64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkrpm -version $(VERSION) -arch riscv64 \
		-bin $(DIST)/$(BINARY)-linux-riscv64 -out $(DIST)/$(BINARY)_$(PKGVERSION)_riscv64.rpm

rpm/arm: linux/arm
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkrpm -version $(VERSION) -arch arm \
		-bin $(DIST)/$(BINARY)-linux-arm -out $(DIST)/$(BINARY)_$(PKGVERSION)_armhfp.rpm

rpm/386: linux/386
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkrpm -version $(VERSION) -arch 386 \
		-bin $(DIST)/$(BINARY)-linux-386 -out $(DIST)/$(BINARY)_$(PKGVERSION)_i386.rpm

rpm: rpm/amd64 rpm/arm64 rpm/riscv64 rpm/arm rpm/386

# macOS .app bundle: tools/mkapp glues the two darwin cross-builds into
# one universal (fat) Mach-O, renders the icns icon set from internal/art
# and packs Contents/{MacOS,Resources} into a deterministic zip — pure
# Go, no Xcode. Also writes the bare universal binary (same bytes as the
# bundle's Contents/MacOS/mario).
app: darwin/amd64 darwin/arm64
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkapp -version $(VERSION) \
		-amd64 $(DIST)/$(BINARY)-darwin-amd64 -arm64 $(DIST)/$(BINARY)-darwin-arm64 \
		-universal $(DIST)/$(BINARY)-darwin-universal \
		-out $(DIST)/$(BINARY)_$(PKGVERSION)_macos.app.zip

# Single-file UEFI bootable: the static init binary (cmd/efi) packed as an
# initramfs (tools/mkcpio) and embedded in an EFI-stub Linux kernel by the
# flake — dist/mario.efi boots straight into the game from any UEFI boot
# menu. Needs nix (the kernel build); x86_64 only. The leaderboard is
# offline in this target (the boot image has no network stack).
 EFI_INIT   := $(DIST)/efi-init
 EFI_INITRD := $(DIST)/init.cpio
# Hermetic flake source: the committed git tree (not path:., which would
# copy dist/ build outputs and .env into the store). From a worktree the
# branch ref resolves through the shared git dir.
EFI_FLAKE  := git+file://$$(pwd)?ref=$$(git branch --show-current)
efi:
	@command -v nix >/dev/null 2>&1 || { echo "make efi: nix is required (builds the EFI-stub kernel)"; exit 1; }
	nix build "$(EFI_FLAKE)#mario-efi" -o $(DIST)/mario-efi-kernel
	cp $(DIST)/mario-efi-kernel/bzImage $(DIST)/mario.efi
	mkdir -p $(DIST)/esp/EFI/BOOT
	cp $(DIST)/mario.efi $(DIST)/esp/EFI/BOOT/BOOTX64.EFI
	@echo "wrote $(DIST)/mario.efi — drop on an ESP, or: make efi-qemu-ovmf"

# Fast dev loop: rebuild just the initramfs from the working tree and
# direct-boot it beside the (nix-cached) kernel under QEMU — no OVMF;
# vesafb 1024x768x16 (the OVMF path exercises efifb 32bpp instead).
efi-initrd:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(EFI_INIT) ./cmd/efi
	CGO_ENABLED=0 go run ./tools/mkcpio $(EFI_INITRD) $(EFI_INIT)

efi-qemu: efi-initrd
	@command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "make efi-qemu: qemu-system-x86_64 not found"; exit 1; }
	nix build "$(EFI_FLAKE)#mario-efi" -o $(DIST)/mario-efi-kernel
	qemu-system-x86_64 -enable-kvm -m 512 -display none -vga std \
	  -kernel $(DIST)/mario-efi-kernel/bzImage -initrd $(EFI_INITRD) \
	  -append "console=tty0 console=ttyS0 vga=791" -serial stdio

# The real thing: boot dist/mario.efi under OVMF firmware (actual UEFI).
# Serial log lands in dist/efi-serial.log; the QEMU monitor is on stdio
# (screendump / sendkey). Quit the game to power off the VM.
efi-qemu-ovmf: efi
	@set -e; \
	 ovmf=$$(nix build --no-link --print-out-paths nixpkgs#OVMF.fd); \
	 code=$$(ls $$ovmf/FV/OVMF_CODE.4m.fd $$ovmf/FV/OVMF_CODE.fd 2>/dev/null | head -n 1); \
	 vars=$$(ls $$ovmf/FV/OVMF_VARS.4m.fd $$ovmf/FV/OVMF_VARS.fd 2>/dev/null | head -n 1); \
	 test -n "$$code" || { echo "OVMF firmware not found in $$ovmf/FV"; exit 1; }; \
	 cp $$vars $(DIST)/OVMF_VARS.fd; chmod 644 $(DIST)/OVMF_VARS.fd; \
	 echo "booting $(DIST)/mario.efi under $$(basename $$code)"; \
	 qemu-system-x86_64 -enable-kvm -m 512 -display none -vga std -monitor stdio \
	   -serial file:$(DIST)/efi-serial.log \
	   -drive if=pflash,format=raw,readonly=on,file=$$code \
	   -drive if=pflash,format=raw,file=$(DIST)/OVMF_VARS.fd \
	   -drive file=fat:rw:$(DIST)/esp,format=raw,if=ide,index=0,media=disk

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

# Regenerate the README screenshots (docs/img) from the current build —
# real rendered frames, no parallel drawing path (tools/shots). The
# rasterizer needs Pillow: pip install pillow, or on NixOS
#   nix-shell -p python3Packages.pillow --run 'make shots'
shots:
	@mkdir -p docs/img dist/shots
	$(GOFLAGS) go build -ldflags '$(LDFLAGS)' -o dist/shots/shots ./tools/shots
	dist/shots/shots -scene title -out dist/shots/title.ansi
	dist/shots/shots -scene play -level 1 -tick 600 -out dist/shots/overworld.ansi
	dist/shots/shots -scene play -level 2 -tick 600 -out dist/shots/underground.ansi
	dist/shots/shots -scene play -level 6 -tick 900 -out dist/shots/sky.ansi
	dist/shots/shots -scene play -level 7 -tick 700 -out dist/shots/castle.ansi
	dist/shots/shots -scene board -out dist/shots/board.ansi
	@for f in title overworld underground sky castle board; do \
		python3 tools/shots/ansi2png.py dist/shots/$$f.ansi docs/img/$$f.png 8 || exit 1; \
	done
	@echo "docs/img refreshed (demo.gif and web-*.png are captured separately)"

web-serve: web
	@echo "serving $(WEBDIST) at http://127.0.0.1:8417/"
	@cd $(WEBDIST) && python3 -m http.server 8417 --bind 127.0.0.1

# Android APK: the WASM web bundle inside a thin WebView shell
# (packaging/android — manifest + one Activity, no Gradle, no NDK).
# Needs an Android SDK (build-tools + a platform android.jar) and a JDK;
# or skip the setup entirely with:  nix develop .#android -c make apk
ANDROID_HOME ?=
AAPT2       ?= $(firstword $(sort $(wildcard $(ANDROID_HOME)/build-tools/*/aapt2)))
D8          ?= $(firstword $(sort $(wildcard $(ANDROID_HOME)/build-tools/*/d8)))
ZIPALIGN    ?= $(firstword $(sort $(wildcard $(ANDROID_HOME)/build-tools/*/zipalign)))
APKSIGNER   ?= $(firstword $(sort $(wildcard $(ANDROID_HOME)/build-tools/*/apksigner)))
ANDROID_JAR ?= $(firstword $(sort $(wildcard $(ANDROID_HOME)/platforms/android-*/android.jar)))
KEYSTORE    := packaging/android/mario.keystore
KSFLAGS     := --ks $(KEYSTORE) --ks-key-alias mario --ks-pass pass:mario-apk --key-pass pass:mario-apk

# Monotonic integer versionCode: vX.Y.Z tags map to X*1e8+Y*1e6+Z*1e4;
# suffixed/dirty working-copy builds fall back to a date code (1yymmdd)
# that stays under every tagged release with a nonzero minor version.
VCODE := $(shell echo "$(PKGVERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' && echo "$(PKGVERSION)" | awk -F. '{print $$1*100000000 + $$2*1000000 + $$3*10000}' || echo 1$$(date +%y%m%d))

apk: web
	@if [ -z "$(AAPT2)" ] || [ -z "$(ANDROID_JAR)" ] || ! command -v javac >/dev/null 2>&1; then \
		echo "ERROR: Android SDK (ANDROID_HOME) + JDK required — or: nix develop .#android -c make apk" >&2; exit 1; \
	fi
	@echo "APK  $(DIST)/$(BINARY)_$(PKGVERSION)_android.apk (versionCode $(VCODE))"
	@rm -rf $(DIST)/apk && mkdir -p $(DIST)/apk/res/mipmap-xxxhdpi $(DIST)/apk/classes
	@cp -r packaging/android/res/. $(DIST)/apk/res/
	@cp web/icons/icon-192.png $(DIST)/apk/res/mipmap-xxxhdpi/ic_launcher.png
	$(AAPT2) compile --dir $(DIST)/apk/res -o $(DIST)/apk/res.zip
	$(AAPT2) link -o $(DIST)/apk/base.apk -I $(ANDROID_JAR) \
		--manifest packaging/android/AndroidManifest.xml \
		--min-sdk-version 26 --target-sdk-version 35 $(if $(APK_DEBUG),--debug-mode,) \
		--version-code $(VCODE) --version-name $(VERSION) \
		-A $(WEBDIST) $(DIST)/apk/res.zip
	javac -source 1.8 -target 1.8 -Xlint:-options -bootclasspath $(ANDROID_JAR) \
		-d $(DIST)/apk/classes packaging/android/java/com/daviey/mario/MainActivity.java
	$(D8) --release --lib $(ANDROID_JAR) --output $(DIST)/apk \
		$(DIST)/apk/classes/com/daviey/mario/*.class
	cd $(DIST)/apk && zip -qj base.apk classes.dex
	$(ZIPALIGN) -f 4 $(DIST)/apk/base.apk $(DIST)/apk/aligned.apk
	$(APKSIGNER) sign $(KSFLAGS) --out $(DIST)/$(BINARY)_$(PKGVERSION)_android.apk \
		$(DIST)/apk/aligned.apk
	$(APKSIGNER) verify --print-certs $(DIST)/$(BINARY)_$(PKGVERSION)_android.apk

# CI pre-release gate, in fail-fast order: formatting, vet, then tests.
check: fmtcheck vet test

# Unsigned iOS .ipa — the WKWebView shell in packaging/ios compiled on
# plain Linux: clang -target arm64-apple-ios + lld against an iPhoneOS
# SDK (.tbd link stubs, no Apple binaries needed), ad-hoc signed with
# ldid, bundled around the web build by tools/mkipa. The SDK is not in
# the repo (Apple licenses it for Apple hardware); point IOS_SDK at an
# extracted one — see packaging/ios/README.md. Sideload the result via
# Sideloadly/AltStore, which re-sign with your own Apple ID.
IOS_SDK ?= $(HOME)/dev/ios-sdks/sdks/iPhoneOS16.5.sdk
ipa: web
	@mkdir -p $(DIST)
	$(GOFLAGS) go run ./tools/mkipa -version $(VERSION) -web $(WEBDIST) -sdk $(IOS_SDK) \
		-out $(DIST)/$(BINARY)_$(PKGVERSION)_ios_unsigned.ipa

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
