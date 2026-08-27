package sshd

// Transport: SSH version exchange, binary packet protocol (RFC 4253 §6),
// and the curve25519-sha256 key exchange (RFC 8731) with an ssh-ed25519
// host key — all from the standard library.
//
// Cipher suite: aes128-ctr + hmac-sha2-256. The classic MAC-then-encrypt
// construction keeps the unencrypted length field readable before the MAC
// is verified, which avoids the AEAD chicken-and-egg on packet framing.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	serverVersion = "SSH-2.0-GoMarioSSH_1.0"

	// Algorithms this server offers (exactly one choice each: the client
	// must support them or fail the exchange — every OpenSSH since 6.5
	// does).
	kexAlgo     = "curve25519-sha256"
	hostkeyAlgo = "ssh-ed25519"
	cipherAlgo  = "aes128-ctr"
	macAlgo     = "hmac-sha2-256"

	maxPacketLen  = 1 << 20 // inbound packet bound; game traffic is ~KBs
	maxPayloadLen = 1 << 18 // our own send bound; callers chunk below it

	macSize     = 32 // hmac-sha2-256
	cipherBlock = 16 // AES; also the post-newkeys padding granularity
	plainBlock  = 8  // pre-newkeys padding granularity

	writeTimeoutSec = 30
)

var errMAC = errors.New("sshd: message authentication failed")

// dirState is one direction of the packet protocol.
type dirState struct {
	stream cipher.Stream // nil until NEWKEYS
	macKey []byte        // nil until NEWKEYS
	seq    uint32
}

// transport reads and writes SSH binary packets over conn.
//
// Concurrency: writes are serialized by wmu (the session's game goroutine
// writes data packets while the reader goroutine writes replies and runs
// rekey bursts). All reads happen on one goroutine at a time — the
// handshake goroutine hands off to the reader before any overlap could
// occur.
type transport struct {
	conn net.Conn

	wmu sync.Mutex
	out dirState
	in  dirState

	vLocal, vPeer []byte // our and the peer's version strings, no CRLF
	ic, is        []byte // raw KEXINIT payloads (for the exchange hash)

	sessionID []byte // fixed at the first exchange, reused on rekey
}

func newTransport(conn net.Conn) *transport { return &transport{conn: conn} }

func deadlineAfter(secs int) time.Time {
	return time.Now().Add(time.Duration(secs) * time.Second)
}

// exchangeVersion sends our identification string and reads the peer's,
// skipping any banner lines that precede it (RFC 4253 §4.2 allows up to
// 255 bytes of them; we tolerate a few lines).
func (t *transport) exchangeVersion(ourVersion string) error {
	if _, err := t.conn.Write([]byte(ourVersion + "\r\n")); err != nil {
		return err
	}
	var line []byte
	one := make([]byte, 1)
	for range 8192 {
		if _, err := io.ReadFull(t.conn, one); err != nil {
			return err
		}
		line = append(line, one[0])
		if one[0] == '\n' {
			l := trimEOL(line)
			if len(l) >= 4 && string(l[:4]) == "SSH-" {
				if len(l) < 7 || (string(l[4:7]) != "2.0" && string(l[4:6]) != "1.") {
					return fmt.Errorf("sshd: unsupported version %q", l)
				}
				t.vPeer = l
				t.vLocal = []byte(ourVersion)
				return nil
			}
			if len(line) > 512 {
				return errors.New("sshd: version line too long")
			}
			line = nil
		}
	}
	return errors.New("sshd: no version string")
}

func trimEOL(l []byte) []byte {
	for len(l) > 0 && (l[len(l)-1] == '\n' || l[len(l)-1] == '\r') {
		l = l[:len(l)-1]
	}
	return l
}

// writePacket sends payload as one binary packet.
func (t *transport) writePacket(payload []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.writePacketLocked(payload)
}

func (t *transport) writePacketLocked(payload []byte) error {
	if len(payload) > maxPayloadLen {
		return errors.New("sshd: payload too large")
	}
	block := plainBlock
	if t.out.stream != nil {
		block = cipherBlock
	}
	pad := block - (5+len(payload))%block
	if pad < 4 {
		pad += block
	}
	total := 4 + 1 + len(payload) + pad
	pkt := make([]byte, total, total+macSize)
	binary.BigEndian.PutUint32(pkt, uint32(1+len(payload)+pad))
	pkt[4] = byte(pad)
	copy(pkt[5:], payload)
	if _, err := rand.Read(pkt[5+len(payload):]); err != nil {
		return err
	}
	if t.out.macKey != nil {
		pkt = append(pkt, macSum(t.out.macKey, t.out.seq, pkt)...)
	}
	if t.out.stream != nil {
		t.out.stream.XORKeyStream(pkt[:total], pkt[:total])
	}
	t.conn.SetWriteDeadline(deadlineAfter(writeTimeoutSec))
	_, err := t.conn.Write(pkt)
	t.out.seq++
	return err
}

// readPacket reads one binary packet and returns its payload.
func (t *transport) readPacket() ([]byte, error) {
	var pkt []byte
	if t.in.stream == nil {
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(t.conn, hdr); err != nil {
			return nil, err
		}
		length := binary.BigEndian.Uint32(hdr)
		if length < 1 || length > maxPacketLen {
			return nil, fmt.Errorf("sshd: bad packet length %d", length)
		}
		pkt = make([]byte, 4+length)
		copy(pkt, hdr)
		if _, err := io.ReadFull(t.conn, pkt[4:]); err != nil {
			return nil, err
		}
	} else {
		hdr := make([]byte, cipherBlock)
		if _, err := io.ReadFull(t.conn, hdr); err != nil {
			return nil, err
		}
		// CTR is a stream: decrypting the header block and the remainder
		// through the same stream is identical to decrypting the whole
		// packet at once.
		t.in.stream.XORKeyStream(hdr, hdr)
		length := binary.BigEndian.Uint32(hdr)
		total := 4 + uint64(length)
		if total%cipherBlock != 0 || total > maxPacketLen {
			return nil, fmt.Errorf("sshd: bad encrypted packet length %d", length)
		}
		pkt = make([]byte, total)
		copy(pkt, hdr)
		if _, err := io.ReadFull(t.conn, pkt[cipherBlock:]); err != nil {
			return nil, err
		}
		t.in.stream.XORKeyStream(pkt[cipherBlock:], pkt[cipherBlock:])
		got := make([]byte, macSize)
		if _, err := io.ReadFull(t.conn, got); err != nil {
			return nil, err
		}
		if !hmac.Equal(got, macSum(t.in.macKey, t.in.seq, pkt)) {
			return nil, errMAC
		}
	}
	length := binary.BigEndian.Uint32(pkt[:4])
	padLen := int(pkt[4])
	if padLen < 4 || padLen >= int(length) {
		return nil, errors.New("sshd: bad padding")
	}
	end := 4 + int(length) - padLen
	payload := append([]byte(nil), pkt[5:end]...)
	t.in.seq++
	return payload, nil
}

func macSum(key []byte, seq uint32, pkt []byte) []byte {
	h := hmac.New(sha256.New, key)
	var s [4]byte
	binary.BigEndian.PutUint32(s[:], seq)
	h.Write(s[:])
	h.Write(pkt)
	return h.Sum(nil)
}

// hostKey is the server's ed25519 identity.
type hostKey struct {
	priv ed25519.PrivateKey
}

func generateHostKey() (*hostKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &hostKey{priv: priv}, nil
}

func (h *hostKey) public() ed25519.PublicKey { return h.priv.Public().(ed25519.PublicKey) }

// publicBlob is the SSH wire encoding of the public key.
func (h *hostKey) publicBlob() []byte {
	w := &buf{}
	w.cstr(hostkeyAlgo)
	w.str(h.public())
	return w.b
}

// sign produces the SSH signature blob over the exchange hash.
func (h *hostKey) sign(hash []byte) []byte {
	sig := ed25519.Sign(h.priv, hash)
	w := &buf{}
	w.cstr(hostkeyAlgo)
	w.str(sig)
	return w.b
}

// fingerprint renders the OpenSSH-style SHA256:<base64> host key id.
func (h *hostKey) fingerprint() string {
	sum := sha256.Sum256(h.publicBlob())
	return "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(sum[:])
}

// buildKexinit assembles our KEXINIT payload with a fresh cookie.
func buildKexinit() []byte {
	var cookie [16]byte
	rand.Read(cookie[:])
	w := &buf{}
	w.u8(msgKexinit)
	w.b = append(w.b, cookie[:]...)
	w.cstr(kexAlgo)     // kex algorithms
	w.cstr(hostkeyAlgo) // server host key algorithms
	w.cstr(cipherAlgo)  // encryption c2s
	w.cstr(cipherAlgo)  // encryption s2c
	w.cstr(macAlgo)     // mac c2s
	w.cstr(macAlgo)     // mac s2c
	w.cstr("none")      // compression c2s
	w.cstr("none")      // compression s2c
	w.cstr("")          // languages c2s
	w.cstr("")          // languages s2c
	w.boolean(false)    // first kex packet does not follow
	w.u32(0)            // reserved
	return w.b
}

func listHas(list, want string) bool {
	for _, s := range splitList(list) {
		if s == want {
			return true
		}
	}
	return false
}

func splitList(list string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(list); i++ {
		if i == len(list) || list[i] == ',' {
			if i > start {
				out = append(out, list[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// kexinitOffers parses a KEXINIT payload just enough to confirm it offers
// every algorithm we require.
func kexinitOffers(p []byte) error {
	if len(p) < 1+16 {
		return errors.New("sshd: malformed KEXINIT")
	}
	r := &reader{b: p[1+16:]} // skip msg byte + raw cookie
	names := make([]string, 10)
	for i := range names {
		names[i] = string(r.str())
	}
	if !r.ok() {
		return errors.New("sshd: malformed KEXINIT")
	}
	want := []string{kexAlgo, hostkeyAlgo, cipherAlgo, cipherAlgo, macAlgo, macAlgo, "none", "none"}
	for i := range want {
		if !listHas(names[i], want[i]) {
			return fmt.Errorf("sshd: client does not offer %s", want[i])
		}
	}
	return nil
}

// serverKex runs one key exchange. The caller has already read the peer's
// KEXINIT payload into t.ic; t.is must be set to the payload we sent.
// After the exchange both directions use fresh keys with reset sequence
// numbers. Packets the peer sends between its KEXINIT and NEWKEYS (legal
// under old keys) are returned for reprocessing.
func (t *transport) serverKex(hk *hostKey) (queued [][]byte, err error) {
	// Read the client's ECDH init. Tolerate ignore/debug noise.
	var e []byte
	for {
		p, err := t.readPacket()
		if err != nil {
			return nil, err
		}
		if len(p) == 0 {
			continue
		}
		if p[0] == msgKexDHInit {
			r := &reader{b: p[1:]}
			e = r.str()
			if !r.ok() || len(e) != 32 {
				return nil, errors.New("sshd: bad ECDH init")
			}
			break
		}
		if p[0] == msgIgnore || p[0] == msgDebug || p[0] == msgUnimplemented {
			continue
		}
		queued = append(queued, p)
	}

	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pub, err := curve.NewPublicKey(e)
	if err != nil {
		return nil, errors.New("sshd: bad client public key")
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, errors.New("sshd: bad curve25519 shared secret")
	}

	ks := hk.publicBlob()
	f := priv.PublicKey().Bytes()

	h := sha256.New()
	w := &buf{}
	w.str(t.vPeer)  // V_C: the client's version
	w.str(t.vLocal) // V_S: ours
	w.str(t.ic)
	w.str(t.is)
	w.str(ks)
	w.str(e)
	w.str(f)
	w.mpint(shared)
	h.Write(w.b)
	hash := h.Sum(nil)

	if t.sessionID == nil {
		t.sessionID = hash
	}

	reply := &buf{}
	reply.u8(msgKexDHReply)
	reply.str(ks)
	reply.str(f)
	reply.str(hk.sign(hash))

	t.wmu.Lock()
	defer t.wmu.Unlock()
	if err := t.writePacketLocked(reply.b); err != nil {
		return nil, err
	}
	if err := t.writePacketLocked([]byte{msgNewkeys}); err != nil {
		return nil, err
	}
	keys := deriveKeys(shared, hash, t.sessionID)
	// From here on our writes use the new keys; the peer's packets keep
	// the old keys until its NEWKEYS arrives.
	enc, _ := aes.NewCipher(keys.encS)
	// Without strict-kex negotiated, sequence numbers are NOT reset at
	// NEWKEYS (modern OpenSSH only resets when kex-strict-* is agreed);
	// the per-direction counter runs for the connection's lifetime.
	t.out = dirState{stream: cipher.NewCTR(enc, keys.ivS), macKey: keys.macS, seq: t.out.seq}

	for {
		p, err := t.readPacket()
		if err != nil {
			return nil, err
		}
		if len(p) > 0 && p[0] == msgNewkeys {
			break
		}
		if len(p) > 0 && (p[0] == msgIgnore || p[0] == msgDebug || p[0] == msgUnimplemented) {
			continue
		}
		queued = append(queued, p)
	}
	dec, _ := aes.NewCipher(keys.encC)
	t.in = dirState{stream: cipher.NewCTR(dec, keys.ivC), macKey: keys.macC, seq: t.in.seq}
	return queued, nil
}

// kdfKeys is the derived key material (RFC 4253 §7.2).
type kdfKeys struct {
	ivC, ivS, encC, encS, macC, macS []byte
}

func deriveKeys(k, h, sessionID []byte) *kdfKeys {
	// mpint-encoded K feeds the KDF hash (RFC 4253 §7.1).
	mw := &buf{}
	mw.mpint(k)
	kmp := mw.b

	expand := func(ch byte, need int) []byte {
		var out []byte
		for len(out) < need {
			hh := sha256.New()
			hh.Write(kmp)
			hh.Write(h)
			hh.Write([]byte{ch})
			hh.Write(sessionID)
			hh.Write(out)
			out = append(out, hh.Sum(nil)...)
		}
		return out[:need]
	}
	return &kdfKeys{
		ivC:  expand('A', 16),
		ivS:  expand('B', 16),
		encC: expand('C', 16),
		encS: expand('D', 16),
		macC: expand('E', 32),
		macS: expand('F', 32),
	}
}
