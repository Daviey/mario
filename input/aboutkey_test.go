package input

import "testing"

func TestAboutKeyIsNotAGameKey(t *testing.T) {
	// 'i' opens the about screen on the title; it must be a mapped no-op
	// for the game — no AnyKey edge (which would start the game), no
	// movement, no specials. Mirrors the 'l' regression.
	m := NewMapper()
	m.Feed([]byte("i"))
	in := m.Poll()
	if in.AnyKey || in.Quit || in.Pause || in.Restart || in.Left || in.Right || in.Up || in.Down || in.Run || in.Suicide {
		t.Errorf("'i' produced game input %+v; want none", in)
	}
}
