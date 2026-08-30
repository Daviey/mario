package sshd

// SSH wire-format primitives (RFC 4251): everything on the wire is
// big-endian; strings carry a uint32 length; mpints are two's-complement
// big-endian with redundant leading bytes stripped.

import "encoding/binary"

// buf builds SSH wire messages.
type buf struct{ b []byte }

func (w *buf) u8(v byte) { w.b = append(w.b, v) }
func (w *buf) u32(v uint32) {
	w.b = binary.BigEndian.AppendUint32(w.b, v)
}
func (w *buf) boolean(v bool) {
	if v {
		w.b = append(w.b, 1)
	} else {
		w.b = append(w.b, 0)
	}
}
func (w *buf) str(s []byte) { w.u32(uint32(len(s))); w.b = append(w.b, s...) }
func (w *buf) cstr(s string) {
	w.u32(uint32(len(s)))
	w.b = append(w.b, s...)
}

// mpint appends the SSH multiple-precision integer encoding of the
// unsigned value v (the only shape this server needs: shared secrets).
func (w *buf) mpint(v []byte) {
	i := 0
	for i < len(v) && v[i] == 0 {
		i++
	}
	v = v[i:]
	if len(v) == 0 {
		w.u32(0)
		return
	}
	if v[0]&0x80 != 0 {
		w.u32(uint32(len(v) + 1))
		w.b = append(w.b, 0)
	} else {
		w.u32(uint32(len(v)))
	}
	w.b = append(w.b, v...)
}

// reader parses SSH wire messages. Failed reads set err; every subsequent
// read is a no-op, so callers can check ok() once at the end.
type reader struct {
	b   []byte
	err bool
}

func (r *reader) u8() byte {
	if r.err || len(r.b) < 1 {
		r.err = true
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *reader) boolean() bool { return r.u8() != 0 }

func (r *reader) u32() uint32 {
	if r.err || len(r.b) < 4 {
		r.err = true
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

// str returns the next length-prefixed byte string.
func (r *reader) str() []byte {
	n := r.u32()
	if r.err || uint64(n) > uint64(len(r.b)) {
		r.err = true
		return nil
	}
	s := r.b[:n]
	r.b = r.b[n:]
	return s
}

// ok reports whether every read so far succeeded; once false it stays
// false (the err latch), so parsers check it once after a run of reads.
func (r *reader) ok() bool { return !r.err }

// SSH message numbers (RFC 4253 §11 / 4254 §5).
const (
	msgDisconnect     = 1
	msgIgnore         = 2
	msgUnimplemented  = 3
	msgDebug          = 4
	msgServiceRequest = 5
	msgServiceAccept  = 6
	msgExtInfo        = 7

	msgKexinit    = 20
	msgNewkeys    = 21
	msgKexDHInit  = 30
	msgKexDHReply = 31

	msgUserauthRequest = 50
	msgUserauthFailure = 51
	msgUserauthSuccess = 52
	msgUserauthBanner  = 53

	msgGlobalRequest   = 80
	msgRequestSuccess  = 81
	msgRequestFailure  = 82
	msgChannelOpen     = 90
	msgChannelOpenConf = 91
	msgChannelOpenFail = 92
	msgWindowAdjust    = 93
	msgChannelData     = 94
	msgChannelExtData  = 95
	msgChannelEOF      = 96
	msgChannelClose    = 97
	msgChannelRequest  = 98
	msgChannelSuccess  = 99
	msgChannelFailure  = 100
)

// Disconnect reason codes (RFC 4253 §11.1) — only the ones sent.
const (
	discProtocolError = 2
	discByApplication = 11
	discTooManyConns  = 12
)
