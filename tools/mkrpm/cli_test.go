package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCLIRunBuildsRPM exercises the REAL CLI path (see mkdeb's twin test):
// main()'s wiring through pack.ParseInputs must survive a process-style
// invocation, because the release matrix drives `make rpm/<goarch>` —
// v0.8.0 shipped a ParseInputs that parsed an empty arg slice when the
// tool main passed no explicit args, and only an executed tool catches it.
func TestCLIRunBuildsRPM(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain for the CLI smoke run")
	}
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "mario_0.0.1_x86_64.rpm")
	cmd := exec.Command("go", "run", ".",
		"-version", "v0.0.1", "-arch", "amd64",
		"-bin", bin, "-pkgdir", pkg, "-out", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run ./tools/mkrpm: %v\n%s", err, b)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if info.Size() < 100 {
		t.Fatalf("artifact implausibly small: %d bytes", info.Size())
	}
}
