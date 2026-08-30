package board

import (
	"crypto/sha256"
	"strconv"
)

// powBits is the proof-of-work difficulty: leading zero bits required in
// sha256("<device_id>:<score>:<nonce>"). 20 bits is ~2^20 hashes on average
// (~0.1s native, well under a second in wasm) — instant for an honest
// player, real CPU cost per row for scripted flooding, so rotating
// device_id no longer buys anything. Keep in sync with verify_pow() in
// supabase/migrations/20260825000003_pow_and_privacy.sql ("00000" hex
// prefix == 20 zero bits).
const powBits = 20

// solvePow returns a nonce (decimal string) whose hash over
// "<device_id>:<score>:<nonce>" carries powBits leading zero bits. The
// nonce must stay a bare decimal string: the server's verify_pow()
// trigger rebuilds exactly that payload text before re-hashing, so any
// other nonce encoding (hex, padded, signed) would fail server-side.
func solvePow(deviceID string, score int) string {
	prefix := []byte(deviceID + ":" + strconv.Itoa(score) + ":")
	for n := uint64(0); ; n++ {
		sum := sha256.Sum256(strconv.AppendUint(prefix[:len(prefix):len(prefix)], n, 10))
		if powOK(sum) {
			return strconv.FormatUint(n, 10)
		}
	}
}

// powOK reports whether h has powBits leading zero bits. The case-20
// test is verify_pow()'s hex check spelled in bytes: h[0] and h[1] zero
// plus a zero top nibble of h[2] is exactly the "00000" prefix the
// migration looks for. Any other difficulty must extend both sides.
func powOK(h [32]byte) bool {
	switch powBits {
	case 20:
		return h[0] == 0 && h[1] == 0 && h[2]>>4 == 0
	default:
		panic("unsupported pow difficulty")
	}
}
