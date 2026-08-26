package persist

// Key-calibration persistence: the input mapper learns the terminal's OS
// key-repeat delay and per-key hold habits (see the input package). Learning
// is stored next to player.json so a fresh run starts warm — without it the
// first hold of each key stalls for the repeat delay every session.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"mario/input"
)

// keyCalibrationPath returns <UserConfigDir>/mario/keys.json, creating the
// directory if needed.
func keyCalibrationPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "mario")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(p, "keys.json"), nil
}

// loadKeyCalibration applies any previously stored learning to the mapper.
// Best-effort: a missing or unreadable file just means starting cold.
func LoadCalibration(m *input.Mapper) {
	path, err := keyCalibrationPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c input.Calibration
	if json.Unmarshal(data, &c) == nil {
		m.ApplyCalibration(c)
	}
}

// saveKeyCalibration stores the mapper's current learning for the next run.
// Best-effort: failures are ignored, same as the player name save.
func SaveCalibration(m *input.Mapper) {
	path, err := keyCalibrationPath()
	if err != nil {
		return
	}
	data, err := json.Marshal(m.Calibration())
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
