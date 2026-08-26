package ui

// ScriptInput is the deterministic demo script: hold right, run most
// ticks, hop regularly, dismiss the title screen on tick 0.

import "mario/engine"

func ScriptInput(t int) engine.Input {
	return engine.Input{
		Right:  true,
		Run:    t%3 != 0,
		Up:     t%97 < 22,
		AnyKey: t == 0, // dismiss the title screen
	}
}
