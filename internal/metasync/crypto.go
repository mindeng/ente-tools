package metasync

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/poly1305"
)

const (
	// TagMessage the most common tag
	TagMessage = 0
	// TagPush indicates the message marks the end of a set of messages
	TagPush = 0x01
	// TagRekey forget the key
	TagRekey = 0x02
	// TagFinal indicates the message marks the end of the stream
	TagFinal = TagPush | TagRekey

	StreamKeyBytes    = chacha20poly1305.KeySize
	StreamHeaderBytes = chacha20poly1305.NonceSizeX
	// XChaCha20Poly1305IetfABYTES links to crypto_secretstream_xchacha20poly1305_ABYTES
	XChaCha20Poly1305IetfABYTES = 16 + 1
)

const cryptoCoreHchacha20InputBytes = 16
const cryptoSecretStreamXchacha20poly1305Counterbytes = 4

var pad0 [16]byte

var invalidKey = errors.New("invalid key")
var invalidInput = errors.New("invalid input")
var cryptoFailure = errors.New("crypto failed")

// streamState represents the secret stream state
type streamState struct {
	k     [StreamKeyBytes]byte
	nonce [chacha20poly1305.NonceSize]byte
	pad   [8]byte
}

func (s *streamState) reset() {
	for i := range s.nonce {
		s.nonce[i] = 0
	}
	s.nonce[0] = 1
}

type decryptor struct {
	streamState
}

// NewDecryptor creates a new decryptor using the key and header
func NewDecryptor(key, header []byte) (*decryptor, error) {
	if len(key) != StreamKeyBytes {
		return nil, invalidKey
	}
	if len(header) != StreamHeaderBytes {
		return nil, invalidInput
	}

	stream := &decryptor{}

	// crypto_core_hchacha20
	k, err := chacha20.HChaCha20(key, header[:16])
	if err != nil {
		return nil, fmt.Errorf("HChaCha20 error: %w", err)
	}
	copy(stream.k[:], k)

	stream.reset()

	// Copy the inonce from header
	copy(stream.nonce[cryptoSecretStreamXchacha20poly1305Counterbytes:],
		header[cryptoCoreHchacha20InputBytes:])

	copy(stream.pad[:], pad0[:])

	return stream, nil
}

// Pull decrypts a message from the stream
func (s *decryptor) Pull(cipher []byte) ([]byte, byte, error) {
	cipherLen := len(cipher)

	if cipherLen < XChaCha20Poly1305IetfABYTES {
		return nil, 0, invalidInput
	}
	mlen := cipherLen - XChaCha20Poly1305IetfABYTES

	var poly1305State [32]byte
	var block [64]byte
	var slen [8]byte

	// crypto_stream_chacha20_ietf(block, sizeof block, state->nonce, state->k);
	chacha, err := chacha20.NewUnauthenticatedCipher(s.k[:], s.nonce[:])
	if err != nil {
		return nil, 0, err
	}
	chacha.XORKeyStream(block[:], block[:])

	// crypto_onetimeauth_poly1305_init(&poly1305_state, block);
	copy(poly1305State[:], block[:])
	poly := poly1305.New(&poly1305State)

	// block[0] = in[0];
	memZero(block[:])
	block[0] = cipher[0]
	chacha.XORKeyStream(block[:], block[:])

	// tag = block[0]
	tag := block[0]
	block[0] = cipher[0]
	if _, err = poly.Write(block[:]); err != nil {
		return nil, 0, err
	}

	// c = in + (sizeof tag)
	c := cipher[1:]
	if _, err = poly.Write(c[:mlen]); err != nil {
		return nil, 0, err
	}
	padLen := (0x10 - len(block) + mlen) & 0xf
	if _, err = poly.Write(pad0[:padLen]); err != nil {
		return nil, 0, err
	}

	// STORE64_LE(slen, (uint64_t) adlen)
	binary.LittleEndian.PutUint64(slen[:], uint64(0))
	if _, err = poly.Write(slen[:]); err != nil {
		return nil, 0, err
	}

	// STORE64_LE(slen, (sizeof block) + mlen)
	binary.LittleEndian.PutUint64(slen[:], uint64(len(block)+mlen))
	if _, err = poly.Write(slen[:]); err != nil {
		return nil, 0, err
	}

	// crypto_onetimeauth_poly1305_final
	mac := poly.Sum(nil)
	memZero(poly1305State[:])

	// stored_mac = c + mlen
	storedMac := c[mlen:]
	if !bytes.Equal(mac, storedMac) {
		memZero(mac)
		return nil, 0, cryptoFailure
	}

	// crypto_stream_chacha20_ietf_xor_ic(m, c, mlen, state->nonce, 2U, state->k)
	m := make([]byte, mlen)
	chacha.XORKeyStream(m, c[:mlen])

	// XOR_BUF(STATE_INONCE(state), mac, crypto_secretstream_xchacha20poly1305_INONCEBYTES)
	xorBuf(s.nonce[cryptoSecretStreamXchacha20poly1305Counterbytes:], mac)
	bufInc(s.nonce[:cryptoSecretStreamXchacha20poly1305Counterbytes])

	return m, tag, nil
}

// decryptChaCha20Poly1305 decrypts data using XChaCha20-Poly1305 secret stream
func decryptChaCha20Poly1305(data []byte, key []byte, nonce []byte) ([]byte, error) {
	decryptor, err := NewDecryptor(key, nonce)
	if err != nil {
		return nil, err
	}
	decoded, tag, err := decryptor.Pull(data)
	if tag != TagFinal {
		return nil, errors.New("invalid tag")
	}
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// SecretBoxOpen decrypts data using NaCl secretbox
func SecretBoxOpen(cipher []byte, nonce []byte, key []byte) ([]byte, error) {
	if len(nonce) != 24 || len(key) != 32 {
		return nil, invalidKey
	}

	var nonceArr [24]byte
	var keyArr [32]byte
	copy(nonceArr[:], nonce)
	copy(keyArr[:], key)

	decrypted, ok := secretbox.Open(nil, cipher, &nonceArr, &keyArr)
	if !ok {
		return nil, invalidKey
	}

	return decrypted, nil
}

// SealedBoxOpen decrypts data using NaCl sealed box (for shared collections)
func SealedBoxOpen(cipherText []byte, publicKey []byte, secretKey []byte) ([]byte, error) {
	if len(cipherText) < 48 {
		return nil, invalidKey
	}

	var pk [32]byte
	var sk [32]byte
	copy(pk[:], publicKey[:32])
	copy(sk[:], secretKey[:32])

	decrypted, ok := box.OpenAnonymous(nil, cipherText, &pk, &sk)
	if !ok {
		return nil, invalidKey
	}

	return decrypted, nil
}

func memZero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func xorBuf(dst, src []byte) {
	minLen := len(dst)
	if len(src) < minLen {
		minLen = len(src)
	}
	for i := 0; i < minLen; i++ {
		dst[i] ^= src[i]
	}
}

func bufInc(b []byte) {
	for i := range b {
		b[i]++
		if b[i] != 0 {
			break
		}
	}
}

// generateNonce generates a random 24-byte nonce
func generateNonce() ([]byte, error) {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return nonce, nil
}