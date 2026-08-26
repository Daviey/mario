package persist

import (
	"os"
	"path/filepath"
	"testing"

	"mario/input"
)

func TestKeyCalibrationRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	m := input.NewMapper()
	// kLeft..kRun: only right is a proven holder, delay measured at 22 ticks.
	m.ApplyCalibration(input.Calibration{OSDelay: 22, HeldHabit: []bool{false, true, false, false, false}})
	SaveCalibration(m)

	if _, err := os.Stat(filepath.Join(dir, "mario", "keys.json")); err != nil {
		t.Fatalf("calibration file: %v", err)
	}
	next := input.NewMapper()
	LoadCalibration(next)
	got := next.Calibration()
	if got.OSDelay != 22 || len(got.HeldHabit) != 5 || !got.HeldHabit[1] {
		t.Fatalf("round trip got %+v", got)
	}
}

func TestKeyCalibrationColdStart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no file: must stay cold, not fail
	m := input.NewMapper()
	LoadCalibration(m)
	if c := m.Calibration(); c.OSDelay != 0 {
		t.Fatalf("cold start unexpectedly calibrated: %+v", c)
	}
	// A corrupt file must also load as cold.
	path := filepath.Join(t.TempDir(), "mario")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keys.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	LoadCalibration(m)
	if c := m.Calibration(); c.OSDelay != 0 {
		t.Fatalf("corrupt file applied: %+v", c)
	}
}
