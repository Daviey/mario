//go:build linux

package main

import (
	"testing"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
)

// Each sequence the evdev layer emits must be understood by the real
// input mapper: a press sets its game input, a release clears it.
func TestKeySeqsDriveMapper(t *testing.T) {
	cases := []struct {
		name string
		code uint16
		want func(engine.Input) bool
	}{
		{"arrow left", evLeft, func(i engine.Input) bool { return i.Left }},
		{"arrow right", evRight, func(i engine.Input) bool { return i.Right }},
		{"arrow up (jump)", evUp, func(i engine.Input) bool { return i.Up }},
		{"arrow down", evDown, func(i engine.Input) bool { return i.Down }},
		{"a moves left", evA, func(i engine.Input) bool { return i.Left }},
		{"d moves right", evD, func(i engine.Input) bool { return i.Right }},
		{"w jumps", evW, func(i engine.Input) bool { return i.Up }},
		{"s ducks", evS, func(i engine.Input) bool { return i.Down }},
		{"x runs", evX, func(i engine.Input) bool { return i.Run }},
		{"space jumps", evSpace, func(i engine.Input) bool { return i.Up }},
		{"p pauses", evP, func(i engine.Input) bool { return i.Pause }},
		{"k suicides", evK, func(i engine.Input) bool { return i.Suicide }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := input.NewMapper()
			press, release := keySeqs(tc.code, false)
			if press == "" || release == "" {
				t.Fatalf("missing press/release for %s (code %d)", tc.name, tc.code)
			}
			m.Feed([]byte(press))
			if got := m.Poll(); !tc.want(got) {
				t.Fatalf("after press, wanted true, got %+v", got)
			}
			m.Feed([]byte(release))
			cleared := false
			for range 40 {
				if !tc.want(m.Poll()) {
					cleared = true
					break
				}
			}
			if !cleared {
				t.Fatal("release did not clear input after 40 ticks")
			}
		})
	}
}

func TestOneShotKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint16
	}{
		{"enter", evEnter},
		{"esc", evEsc},
		{"backspace", evBackspace},
	} {
		press, release := keySeqs(tc.code, false)
		if press == "" {
			t.Fatalf("missing press for %s", tc.name)
		}
		if release != "" {
			t.Errorf("%s: wanted press-only, got release %q", tc.name, release)
		}
	}
}

func TestShiftUppercase(t *testing.T) {
	press, _ := keySeqs(evD, true)
	want := "\x1b[68;1:1u" // 'D'
	if press != want {
		t.Errorf("shifted 'd' = %q, want %q", press, want)
	}
	m := input.NewMapper()
	m.Feed([]byte(press))
	if !m.Poll().Right {
		t.Error("'D' not mapped to Right")
	}
}
