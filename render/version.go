package render

import "strings"

// Version is the build identity drawn under the title-screen subtitle,
// injected at link time (-ldflags -X mario/render.Version=...) from
// `git describe --tags --always --dirty`: an exact tagged build prints just
// the tag, commits past the tag append <n>-g<hash>, a dirty tree appends
// -dirty. The default labels ad-hoc builds; the empty string draws nothing.
var Version = "DEV"

// versionCandidates ladders a raw git-describe string into progressively
// shorter arcade-safe forms for pickTextPx: the full string first, then the
// describe without its commit-hash segment, then the bare tag. Narrow
// viewports automatically fall through to the shorter variants.
func versionCandidates(v string) []string {
	s := sanitizeArcade(v)
	if s == "" {
		return nil
	}
	segs := strings.Split(s, "-")
	cands := []string{s}
	kept := make([]string, 0, len(segs))
	for _, seg := range segs {
		if len(seg) > 1 && seg[0] == 'G' && isHexUpper(seg[1:]) {
			continue // describe's g<hash> commit segment
		}
		kept = append(kept, seg)
	}
	if noHash := strings.Join(kept, "-"); noHash != s {
		cands = append(cands, noHash)
	}
	if tag := segs[0]; tag != cands[len(cands)-1] {
		cands = append(cands, tag)
	}
	return cands
}

// sanitizeArcade upper-cases s and strips every rune outside the pixel
// font's charset (A-Z 0-9 space . - + / ! ?), so arbitrary tag names can
// never reach a missing glyph.
func sanitizeArcade(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == ' ', r == '.', r == '-', r == '+', r == '/', r == '!', r == '?':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isHexUpper(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
