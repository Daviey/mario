package pgwire

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const scramMechanism = "SCRAM-SHA-256"

// pbkdf2SHA256 derives keyLen bytes from password and salt using
// PBKDF2-HMAC-SHA-256 (RFC 2898). The stdlib has no PBKDF2.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	var dk []byte
	for block := 1; len(dk) < keyLen; block++ {
		prf.Reset()
		prf.Write(salt)
		var idx [4]byte
		binary.BigEndian.PutUint32(idx[:], uint32(block))
		prf.Write(idx[:])
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// scramClient runs one SCRAM-SHA-256 exchange (RFC 5802 with SHA-256 per
// RFC 7677). The nonce is injected so tests can pin it to the RFC 7677
// test vector; production callers pass fresh crypto/rand bytes.
type scramClient struct {
	user        string
	password    string
	nonce       string // wire (base64) form of the client nonce
	serverFirst string
	serverNonce string
	salt        []byte
	iter        int

	clientKey []byte
	storedKey []byte
	serverKey []byte
	serverSig []byte
}

func newScramClient(user, password, nonce string) *scramClient {
	return &scramClient{user: user, password: password, nonce: nonce}
}

// clientFirst returns the full client-first message (gs2 header
// included): "n,,n=<user>,r=<nonce>".
func (s *scramClient) clientFirst() string {
	return "n,," + s.clientFirstBare()
}

func (s *scramClient) clientFirstBare() string {
	return "n=" + s.user + ",r=" + s.nonce
}

// setServerFirst parses "r=<combined nonce>,s=<b64 salt>,i=<iterations>"
// and derives the SCRAM keys.
func (s *scramClient) setServerFirst(msg string) error {
	s.serverFirst = msg
	for _, attr := range strings.Split(msg, ",") {
		if len(attr) < 2 || attr[1] != '=' {
			return fmt.Errorf("pgwire: bad scram attribute %q", attr)
		}
		val := attr[2:]
		switch attr[0] {
		case 'r':
			if !strings.HasPrefix(val, s.nonce) {
				return errors.New("pgwire: scram server nonce does not extend client nonce")
			}
			s.serverNonce = val
		case 's':
			b, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return fmt.Errorf("pgwire: scram salt: %w", err)
			}
			s.salt = b
		case 'i':
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return fmt.Errorf("pgwire: bad scram iteration count %q", val)
			}
			s.iter = n
		}
	}
	if s.serverNonce == "" || len(s.salt) == 0 || s.iter == 0 {
		return errors.New("pgwire: incomplete scram server-first message")
	}
	salted := pbkdf2SHA256([]byte(s.password), s.salt, s.iter, sha256.Size)
	s.clientKey = hmacSHA256(salted, []byte("Client Key"))
	stored := sha256.Sum256(s.clientKey)
	s.storedKey = stored[:]
	s.serverKey = hmacSHA256(salted, []byte("Server Key"))
	return nil
}

// clientFinal returns "c=biws,r=<server nonce>,p=<b64 proof>". "biws" is
// base64 of the "n,," gs2 header (no channel binding).
func (s *scramClient) clientFinal() string {
	noProof := "c=biws,r=" + s.serverNonce
	authMsg := s.clientFirstBare() + "," + s.serverFirst + "," + noProof
	clientSig := hmacSHA256(s.storedKey, []byte(authMsg))
	s.serverSig = hmacSHA256(s.serverKey, []byte(authMsg))
	proof := make([]byte, len(s.clientKey))
	for i := range proof {
		proof[i] = s.clientKey[i] ^ clientSig[i]
	}
	return noProof + ",p=" + base64.StdEncoding.EncodeToString(proof)
}

// verifyServerFinal checks the server-final "v=<b64 signature>" value
// against the expected ServerSignature. A mismatch must never let the
// exchange continue.
func (s *scramClient) verifyServerFinal(msg string) bool {
	v, ok := strings.CutPrefix(msg, "v=")
	if !ok || s.serverSig == nil {
		return false
	}
	got, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, s.serverSig) == 1
}
