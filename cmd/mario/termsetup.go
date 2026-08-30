package main

import "github.com/Daviey/mario/render"

// The byte-exact terminal setup and teardown shared by the native runner
// (run) and the SSH host (playSession). ORDER IS LOAD-BEARING (kitty
// spec, Quickstart): the keyboard stack is screen-scoped, so the
// prologue pushes AFTER entering the alt screen and the epilogue pops
// BEFORE leaving it — the pop must land while still on the alt screen
// whose stack holds our push; popping after the exit would target the
// main screen's stack instead. (The old SSH prologue pushed on the main
// screen's stack, so the epilogue's pop was a no-op and the pushed level
// survived the exit, leaving the player's shell emitting CSI-u garbage.)
// The prologue's leading pop heals any mode left over from a previous
// run that was killed without cleanup; the epilogue ends synchronized
// output first, restores cursor and title, and only then exits the alt
// screen with a final SGR reset + CRLF so the shell prompt starts clean.
const (
	termPrologue = "\x1b[<u\x1b[?1049h\x1b[>11u\x1b[?25l\x1b[2J\x1b[?22t\x1b]0;SUPER CLI MARIO\a"
	termEpilogue = "\x1b[?2026l\x1b[<u\x1b[?25h\x1b[?23t\x1b[?1049l\x1b[0m\r\n"
)

// viewFor fits the game viewport to a terminal size in character cells:
// width in tiles, height minus the two HUD/status text rows, at Pix/2
// terminal rows per tile (Pix pixels drawn as half-blocks). Degenerate
// sizes flow through unreduced — the 20x5 pty yields 3x1 — because the
// clamps downstream (App.Resize: 16..60 wide, 4+ tall; the engine's
// level bounds) are what keep tiny terminals playable.
func viewFor(cols, rows int) (viewW, viewH int) {
	return cols / render.Pix, (rows - 2) * 2 / render.Pix
}
