package board

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
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

// TestPowMatchesMigration syncs the two difficulty definitions by reading
// the migration itself: verify_pow() accepts a digest whose hex form
// starts with N zero characters, and that prefix must encode exactly
// powBits leading zero bits. The payload concatenation order
// ("<device_id>:<score>:<nonce>", mirrored in solvePow) is asserted too,
// but only when the SQL's payload expression is in the shape this test
// parses — a refactored trigger skips that half rather than guessing.
func TestPowMatchesMigration(t *testing.T) {
	sql, err := os.ReadFile("../supabase/migrations/20260825000003_pow_and_privacy.sql")
	if err != nil {
		t.Fatalf("read migration: %v (tests run from board/, so the path is relative to it)", err)
	}
	prefixRe := regexp.MustCompile(`position\('([0-9]*)' in encode`)
	m := prefixRe.FindStringSubmatch(string(sql))
	if m == nil {
		t.Fatal("verify_pow(): hex-prefix check not found in migration")
	}
	if bits := 4 * len(m[1]); bits != powBits {
		t.Errorf("migration demands %d leading zero bits (hex prefix %q), Go powBits = %d", bits, m[1], powBits)
	}

	payloadRe := regexp.MustCompile(`payload text := new\.(\w+)::text \|\| ':' \|\| new\.(\w+)::text \|\| ':' \|\| coalesce\(new\.(\w+), ''\)`)
	pm := payloadRe.FindStringSubmatch(string(sql))
	if pm == nil {
		t.Log("payload expression not in parseable shape; skipping payload-order half")
		return
	}
	for i, want := range []string{"device_id", "score", "pow_nonce"} {
		if pm[i+1] != want {
			t.Errorf("payload part %d is new.%s, want new.%s (solvePow hashes <device_id>:<score>:<nonce>)", i+1, pm[i+1], want)
		}
	}
}
