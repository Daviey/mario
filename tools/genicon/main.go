// genicon renders the PWA icons (web/icons/*.png) from the classic 5×5
// boot-screen Mario sprite — the same art as the loader in web/index.html
// and render/sprites.go, so the home-screen icon, the favicon and the boot
// screen all show the same face. Stdlib only; run from the repo root:
//
//	CGO_ENABLED=0 go run ./tools/genicon
//
// It can also emit a single icon at any size (used by distro packaging,
// e.g. a 48×48 hicolor icon for .deb / AUR):
//
//	CGO_ENABLED=0 go run ./tools/genicon -out icon48.png -size 48 -cell 8
//
// The web PNGs are committed; regenerate only when the sprite art changes.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/Daviey/mario/internal/art"
)

func write(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	out := flag.String("out", "", "write one icon to this path (with -size/-cell) instead of the web icon set")
	size := flag.Int("size", 0, "icon size in px (with -out)")
	cell := flag.Int("cell", 0, "art pixel size in px (with -out)")
	flag.Parse()

	if *out != "" {
		if *size <= 0 || *cell <= 0 {
			fmt.Fprintln(os.Stderr, "genicon: -out needs -size and -cell > 0")
			os.Exit(2)
		}
		if err := write(*out, art.Icon(*size, *cell)); err != nil {
			fmt.Fprintln(os.Stderr, "genicon:", err)
			os.Exit(1)
		}
		fmt.Println("wrote", *out)
		return
	}

	dir := filepath.Join("web", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
	for _, ic := range []struct {
		name string
		size int
		cell int
	}{
		{"icon-192.png", 192, 32},          // 160 px art, ~83%
		{"icon-512.png", 512, 85},          // 425 px art, ~83%
		{"icon-maskable-512.png", 512, 61}, // 305 px art, ~60% safe zone
	} {
		p := filepath.Join(dir, ic.name)
		if err := write(p, art.Icon(ic.size, ic.cell)); err != nil {
			fmt.Fprintln(os.Stderr, "genicon:", err)
			os.Exit(1)
		}
		fmt.Println("wrote", p)
	}
}
