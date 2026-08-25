package board

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
)

func TestSolvePowMeetsDifficulty(t *testing.T) {
	nonce := solvePow("aaaaaaaa-0000-0000-0000-000000000001", 12500)
	sum := sha256.Sum256([]byte("aaaaaaaa-0000-0000-0000-000000000001:12500:" + nonce))
	if hex.EncodeToString(sum[:])[:5] != "00000" {
		t.Fatalf("nonce %s does not meet difficulty: %x", nonce, sum)
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
