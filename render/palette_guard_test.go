package render

import (
	"reflect"
	"testing"
)

// TestPaletteFullyPopulated guards against a palette entry silently
// dropping out of NewPalette (happened once: a botched edit removed
// BrickDark from the base palette — themes still set it, so only the
// overworld rendered with a zero color, which serializes as the
// terminal default). Every field of the base palette must be a real
// color with a cube fallback.
func TestPaletteFullyPopulated(t *testing.T) {
	p := reflect.ValueOf(*NewPalette(Colors24))
	typ := p.Type()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type != reflect.TypeOf(Color{}) {
			continue // the Colors int
		}
		c := p.Field(i).Interface().(Color)
		if c.RGB == 0 || c.Idx256 == 0 {
			t.Errorf("palette field %s is a zero Color (dropped entry?)", f.Name)
		}
	}
}
