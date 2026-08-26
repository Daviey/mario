<p align="center">
  <img src="docs/img/logo.png" width="128" alt="SUPER CLI MARIO logo" />
</p>

<h1 align="center">SUPER CLI MARIO</h1>

<p align="center">
  A complete Super Mario Bros–style platformer that runs <b>in your terminal</b> —<br />
  and in the browser, on your phone, as a <code>.deb</code>/AUR/Nix package, and bootable on bare metal via UEFI.
</p>

<p align="center">
  <a href="https://github.com/Daviey/mario/actions/workflows/ci.yml"><img src="https://github.com/Daviey/mario/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Daviey/mario/releases/latest"><img src="https://img.shields.io/github/v/tag/Daviey/mario?label=release&sort=semver" alt="latest release" /></a>
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go" alt="Go 1.22+" />
  <img src="https://img.shields.io/badge/dependencies-0-2496ed" alt="zero dependencies" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT license" /></a>
</p>

<p align="center">
  <img src="docs/img/demo.gif" alt="Gameplay: running and jumping through World 1-1, stomping goombas, bumping blocks" />
</p>

Seven hand-built levels across two worlds and four themes — overworld, underground, sky and castle — with mushrooms, fire flowers, star power, hidden blocks, piranha plants, paratroopas, fire bars and lava. All of it drawn as **true-color pixel art using half-block terminal glyphs**: two square pixels per character cell, a custom 3×5 arcade font for the HUD, and a graceful 16-color fallback for basic terminals.

The engine is **fully deterministic** — no randomness, no wall clock — so the same input sequence always reproduces the same game. That's not just a party trick: it's the backbone of an **online leaderboard where every score is verified by replay** (more on that below).

## Screenshots

<p>
  <img src="docs/img/title.png" width="49%" alt="Title screen with pixel-art logo, PRESS ANY KEY prompt" />
  <img src="docs/img/board.png" width="49%" alt="Online leaderboard: ranks, names, scores, level reached, verified checkmarks" />
</p>

| Overworld (1-1) | Underground (1-2) |
|:---:|:---:|
| <img src="docs/img/overworld.png" width="100%" alt="Mario mid-jump over pipes and blocks with goombas" /> | <img src="docs/img/underground.png" width="100%" alt="Underground level: blue bricks, ceiling, koopa and coins" /> |

| Sky (2-3) | Castle (2-4) |
|:---:|:---:|
| <img src="docs/img/sky.png" width="100%" alt="Sky level: sandstone platforms, coins, enemies, mid-air jump" /> | <img src="docs/img/castle.png" width="100%" alt="Castle level: fire bar and lava, jumping the hazard" /> |

The same code compiles to a browser build (Go → WebAssembly, installable PWA with offline support, WebAudio sound and its own touch gamepad on phones):

<p>
  <img src="docs/img/web-desktop.png" width="59%" alt="The game running in a desktop browser" />
  <img src="docs/img/web-mobile.png" width="39%" alt="The game running in a phone browser with the on-screen gamepad" />
</p>

## Quick start

**Terminal** (Linux/macOS/Windows, x86-64 + arm64):

```sh
git clone https://github.com/Daviey/mario && cd mario
make run          # or: make build && ./mario
```

Or grab a static binary, a `.deb`, the Android APK or the web bundle from the
[latest release](https://github.com/Daviey/mario/releases/latest). Nix users:
`nix build` from the flake in this repo; Arch users: `makepkg -p packaging/aur/PKGBUILD`.

**Browser:** play it at **[daviey.github.io/mario](https://daviey.github.io/mario/)** — no install, works offline once loaded, touch controls on phones.

**Bare metal:** `make efi` produces a single-file UEFI executable that boots straight into the game — no OS required.

### Controls

| Key | Action |
|---|---|
| `a`/`d` or arrows | move |
| `w`, space, up | jump |
| `x` (hold) | run · fire when powered |
| `p` / `q` / `k` | pause · quit · die on demand |
| `l` | leaderboard (from the title screen) |
| `d` | daily challenge (from the title screen) |
| `r` | restart after game over |

Play your own levels, too — levels are plain ASCII text files: `./mario -level mylevel.txt`.

## A leaderboard nobody can cheat

Every run is recorded as a compressed input log. When you submit a score, the
recording goes with it, and a verifier **replays your inputs against the same
deterministic engine and keeps the row only if it reproduces the claimed
score, level and engine version**:

```mermaid
flowchart LR
    A[You play] -->|inputs recorded| B[Submit score + recording]
    B --> C{GitHub Action<br/>replays the recording}
    C -->|score matches| D[✓ verified row]
    C -->|mismatch| E[row deleted]
```

Submissions sit behind proof-of-work, per-IP and per-device rate limits, and
row-level security — the board itself is a plain Supabase/PostgREST table, so
there's no game server to run or trust. Verified rows carry a green ✓ on every
board surface (terminal, web, APK).

**Daily mode** generates a new challenge level every day from the date seed —
same level for everyone, structurally checked to be solvable, with its own
leaderboard.

## How it's built

* **Zero dependencies.** The whole game — engine, renderer, leaderboard
  client, WASM target, even the `.deb` packager — is Go standard library.
* **Terminal renderer.** Truecolor half-block pixels, diffed per frame; falls
  back to 16-color ANSI. Speaks the kitty keyboard protocol for real
  press/repeat/release events, with legacy key-repeat inference for terminals
  that don't.
* **Deterministic engine.** Fixed 60 Hz tick, pure `Game.Update(Input)`
  transition — the determinism tests byte-compare full rendered runs, and the
  replay verifier leans on the same property.
* **One codebase, many targets.** `make release` cross-compiles five OS/arch
  pairs; `make web` produces the static PWA; `make apk` wraps it for Android;
  `make deb` / `packaging/aur` / `flake.nix` package it for distros; `make efi`
  builds the bootable image. The Windows exe icon is rendered from the game's
  own sprite data.
* **Importable as a library.** The root package is a facade — `mario.New`,
  `Feed`, `Step`, `Run` — so you can embed the game as an easter egg in your
  own Go program.

## Development

```sh
make build     # native binary (CGO off, fully static)
make check     # fmtcheck + vet + test — the CI gate
make race      # tests under the race detector
make cover     # coverage summary
make web       # static browser build in dist/web
make test      # go test ./...
```

Stdlib `testing` only; assertions are programmatic (pixel reads, ANSI output
contains, geometry checks). The suite includes determinism tests, viewport
size sweeps, and offline fakes for the leaderboard backend.

## License

[MIT](LICENSE)
