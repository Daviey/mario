package main

import (
	"encoding/json"
	"testing"

	"mario/engine"
)

func TestEncodeDecodeInputRoundtrip(t *testing.T) {
	// All 2^9 combinations: every bit must survive independently.
	for v := range 1 << 9 {
		got := decodeInput(uint16(v))
		if back := encodeInput(got); back != uint16(v) {
			t.Fatalf("roundtrip v=%09b -> %+v -> %09b", v, got, back)
		}
	}
}

func TestDecodeInputBits(t *testing.T) {
	in := decodeInput(bitLeft | bitRun | bitQuit | bitAnyKey)
	want := engine.Input{Left: true, Run: true, Quit: true, AnyKey: true}
	if in != want {
		t.Fatalf("decodeInput = %+v, want %+v", in, want)
	}
}

func TestRecorderSubmittable(t *testing.T) {
	r := newRecorder()
	if r.submittable() {
		t.Fatal("empty recording must not be submittable")
	}
	r.record(engine.Input{AnyKey: true}) // title dismiss
	r.record(engine.Input{Right: true})
	if !r.submittable() {
		t.Fatal("short recording should be submittable")
	}
	if got := r.rec.I; len(got) != 2 || got[0] != bitAnyKey || got[1] != bitRight {
		t.Fatalf("recorded inputs = %v", got)
	}
}

func TestRecorderCapInvalidates(t *testing.T) {
	r := newRecorder()
	for range maxRecordTicks + 1 {
		r.record(engine.Input{})
	}
	if r.submittable() {
		t.Fatal("recording past the cap must not be submittable")
	}
	if len(r.rec.I) != maxRecordTicks {
		t.Fatalf("stored %d ticks, want exactly the cap %d", len(r.rec.I), maxRecordTicks)
	}
}

func TestRecordingJSONShape(t *testing.T) {
	r := newRecorder()
	r.record(engine.Input{AnyKey: true})
	r.record(engine.Input{Right: true, Run: true})
	data, err := json.Marshal(r.rec)
	if err != nil {
		t.Fatal(err)
	}
	// Wire format is a schema contract with the verifier and the DB.
	want := `{"v":1,"i":[256,18]}`
	if string(data) != want {
		t.Fatalf("wire format = %s, want %s", data, want)
	}
	var back recording
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.valid() {
		t.Fatal("decoded recording must be valid")
	}
}

func TestRecordingValidRejects(t *testing.T) {
	cases := []recording{
		{},                     // empty
		{V: 1},                 // no inputs
		{V: 2, I: []uint16{1}}, // wrong version
		{V: 1, I: make([]uint16, maxRecordTicks+1)},
	}
	for i, rec := range cases {
		if rec.valid() {
			t.Errorf("case %d: %+v should be invalid", i, rec.V)
		}
	}
}
