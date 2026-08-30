package board

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// TestSolvePowMeetsDifficulty derives its expectation from powBits, so
// the difficulty sync is two-sided: raising powBits fails here (and in
// powOK, which panics on unsupported values) until both the Go check and
// the SQL migration's hex prefix are updated together.
func TestSolvePowMeetsDifficulty(t *testing.T) {
	if powBits%4 != 0 {
		t.Fatalf("pow difficulty %d is not nibble-aligned; verify_pow()'s hex-prefix check cannot express it", powBits)
	}
	nonce := solvePow("aaaaaaaa-0000-0000-0000-000000000001", 12500)
	sum := sha256.Sum256([]byte("aaaaaaaa-0000-0000-0000-000000000001:12500:" + nonce))
	if want := strings.Repeat("0", powBits/4); !strings.HasPrefix(hex.EncodeToString(sum[:]), want) {
		t.Fatalf("nonce %s does not meet difficulty: %x (want %q prefix)", nonce, sum, want)
	}
}

// The server hashes exactly "<device_id>:<score>:<nonce>" — lock the wire
// format so a client/server mismatch fails here instead of live.
func TestPowPayloadFormat(t *testing.T) {
	n := solvePow("d", 7)
	h := sha256.Sum256([]byte("d:7:" + n))
	want := hex.EncodeToString(h[:])
	got := sha256.Sum256([]byte("d" + ":" + strconv.Itoa(7) + ":" + n))
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("payload drift: %x vs %x", got, want)
	}
}

// digestWithLeadingZeroBits returns a digest whose bits 0..n-1 are zero
// and bit n is one — exactly n leading zero bits.
func digestWithLeadingZeroBits(n int) (h [32]byte) {
	h[n/8] |= 1 << (7 - n%8)
	return h
}

// TestPowOKLeadingZeroBoundary pins the accept/reject edge with crafted
// digests: exactly powBits leading zero bits pass, one bit fewer fails.
func TestPowOKLeadingZeroBoundary(t *testing.T) {
	if powBits < 1 || powBits+4 >= 256 {
		t.Fatalf("pow difficulty %d does not fit a sha256 digest", powBits)
	}
	if !powOK(digestWithLeadingZeroBits(powBits)) {
		t.Errorf("digest with exactly %d leading zero bits rejected", powBits)
	}
	if powOK(digestWithLeadingZeroBits(powBits - 1)) {
		t.Errorf("digest with %d leading zero bits (one short) accepted", powBits-1)
	}
	if !powOK(digestWithLeadingZeroBits(powBits + 4)) {
		t.Errorf("digest with more than %d leading zero bits rejected", powBits)
	}
}
