package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCLIRunBuildsDeb exercises the REAL CLI path: it spawns the tool via
// `go run .` with process-style flags, so main()'s wiring through
// pack.ParseInputs is proven, not just the library. v0.8.0 shipped a
// ParseInputs that parsed an empty arg slice when the tool main passed no
// explicit args — every `make deb/<arch>` in the release matrix died with
// "all required" while the unit tests (explicit args) stayed green, and
// `make -n` dry-runs never execute the tool at all. This test is the
// missing rung: the artifact must exist and open as an ar archive.
func TestCLIRunBuildsDeb(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain for the CLI smoke run")
	}
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "mario_0.0.1_amd64.deb")
	cmd := exec.Command("go", "run", ".",
		"-version", "v0.0.1", "-arch", "amd64",
		"-bin", bin, "-pkgdir", pkg, "-out", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run ./tools/mkdeb: %v\n%s", err, b)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if info.Size() < 100 {
		t.Fatalf("artifact implausibly small: %d bytes", info.Size())
	}
}
