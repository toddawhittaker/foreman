package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// MinKeyLength is the shortest FOREMAN_SECRET_KEY accepted (SPEC 9.11).
const MinKeyLength = 32

const (
	// versionByte starts every sealed value so the format can change later.
	versionByte = 0x01
	// hkdfInfo ties the derived key to this use.
	hkdfInfo = "foreman stored secrets v1"
)

// Errors from NewBox and Open. None of them carries the key or the value.
var (
	ErrKeyTooShort = errors.New("the secret key is shorter than 32 characters")
	ErrBadSealed   = errors.New("the stored value is not in a known format")
	ErrCannotOpen  = errors.New("the stored value cannot be opened: wrong key, wrong name, or changed")
)

// Box seals and opens stored secrets with AES-GCM (STACK 7).
type Box struct {
	aead cipher.AEAD
}

// NewBox derives the encryption key from the secret key with HKDF-SHA256.
func NewBox(key Secret) (*Box, error) {
	k := key.Reveal()
	if len(k) < MinKeyLength {
		return nil, ErrKeyTooShort
	}
	derived, err := hkdf.Key(sha256.New, []byte(k), nil, hkdfInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("derive the encryption key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("set up the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("set up the cipher: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext as version byte, nonce, ciphertext. The row name is
// authenticated, so a sealed value cannot be moved to another row.
func (b *Box) Seal(name string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("make a nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, versionByte)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plaintext, []byte(name)), nil
}

// Open reverses Seal. It returns an error, never a panic, for a short value,
// an unknown version, a wrong key, a wrong name or a changed byte.
func (b *Box) Open(name string, sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != versionByte {
		return nil, ErrBadSealed
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(name))
	if err != nil {
		return nil, ErrCannotOpen
	}
	return plain, nil
}
