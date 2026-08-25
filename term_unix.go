//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// rawMode puts the terminal into raw no-echo mode via stty and returns a
// restore function.
func rawMode() (func(), error) {
	stty, err := exec.LookPath("stty")
	if err != nil {
		return nil, err
	}
	old, err := sttyOutput(stty, "-g")
	if err != nil {
		return nil, err
	}
	saved := strings.TrimSpace(old)
	c := exec.Command(stty, "raw", "-echo")
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		return nil, err
	}
	return func() {
		r := exec.Command(stty, saved)
		r.Stdin = os.Stdin
		_ = r.Run()
	}, nil
}

func sttyOutput(path string, args ...string) (string, error) {
	c := exec.Command(path, args...)
	c.Stdin = os.Stdin
	out, err := c.Output()
	return string(out), err
}

func termSize() (rows, cols int) {
	out, err := sttyOutput("stty", "size")
	if err != nil {
		return 0, 0
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0
	}
	cols, err1 := strconv.Atoi(f[1])
	rows, err2 := strconv.Atoi(f[0])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return rows, cols
}

// termWidth returns the terminal width in columns (0 when unknown).
func termWidth() int { _, c := termSize(); return c }

// termHeight returns the terminal height in rows (0 when unknown).
func termHeight() int { r, _ := termSize(); return r }
