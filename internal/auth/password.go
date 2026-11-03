// Package auth holds the sign-in building blocks: password hashing, the
// failed-sign-in limiter, client address and trusted-header handling, and
// server-side sessions (SPEC 12.3 to 12.8). It has no HTTP routes.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Current argon2id settings: OWASP's minimum for argon2id (STACK 7).
const (
	memoryKiB = 19456
	passes    = 2
	threads   = 1
	saltLen   = 16
	keyLen    = 32

	MinPasswordChars = 12
	MaxPasswordBytes = 1024

	// Upper bounds on parsed parameters, so a corrupt stored hash cannot
	// make a sign-in allocate gigabytes.
	maxMemoryKiB = 1 << 20
	maxPasses    = 16
	maxThreads   = 16
	maxKeyBytes  = 1024
)

var b64 = base64.RawStdEncoding

// ErrPasswordLength is returned for a password outside the allowed length.
// Its message is safe to show to the user.
var ErrPasswordLength = fmt.Errorf("the password must be at least %d characters and at most %d bytes", MinPasswordChars, MaxPasswordBytes)

// CheckPassword reports whether a new password is acceptable.
func CheckPassword(pw string) error {
	if len(pw) > MaxPasswordBytes || utf8.RuneCountInString(pw) < MinPasswordChars {
		return ErrPasswordLength
	}
	return nil
}

// Hash returns the argon2id PHC string for a new password.
func Hash(pw string) (string, error) {
	if err := CheckPassword(pw); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read random salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, passes, memoryKiB, threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, passes, threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

type phc struct {
	memory, passes uint32
	threads        uint8
	salt, key      []byte
}

func parsePHC(encoded string) (phc, error) {
	var p phc
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, errors.New("auth: stored password hash is not an argon2id string")
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, errors.New("auth: unsupported argon2 version")
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 ||
		!strings.HasPrefix(fields[0], "m=") || !strings.HasPrefix(fields[1], "t=") || !strings.HasPrefix(fields[2], "p=") {
		return p, errors.New("auth: malformed argon2 parameters")
	}
	m, err := strconv.ParseUint(fields[0][2:], 10, 32)
	if err != nil {
		return p, errors.New("auth: malformed argon2 memory")
	}
	t, err := strconv.ParseUint(fields[1][2:], 10, 32)
	if err != nil {
		return p, errors.New("auth: malformed argon2 passes")
	}
	th, err := strconv.ParseUint(fields[2][2:], 10, 8)
	if err != nil {
		return p, errors.New("auth: malformed argon2 threads")
	}
	if m == 0 || m > maxMemoryKiB || t == 0 || t > maxPasses || th == 0 || th > maxThreads {
		return p, errors.New("auth: argon2 parameters out of range")
	}
	if p.salt, err = b64.DecodeString(parts[4]); err != nil || len(p.salt) == 0 {
		return p, errors.New("auth: malformed argon2 salt")
	}
	if p.key, err = b64.DecodeString(parts[5]); err != nil || len(p.key) == 0 || len(p.key) > maxKeyBytes {
		return p, errors.New("auth: malformed argon2 key")
	}
	p.memory, p.passes, p.threads = uint32(m), uint32(t), uint8(th)
	return p, nil
}

// maxHashing caps password checks running at once, so a burst of sign-ins
// cannot exhaust memory: each check allocates about 19 MiB.
const maxHashing = 4

var hashSlots = make(chan struct{}, maxHashing)

// ErrBusy means too many password checks are already running and this one
// did not run. The caller should ask the user to try again shortly.
var ErrBusy = errors.New("auth: too many password checks at once")

// Verify checks a password against a stored hash in constant time.
// needsRehash is true when the hash was made with settings below the
// current ones, so the caller can store a fresh hash after a good sign-in.
// An error means the stored string is unusable or, for ErrBusy, that no
// check ran; never that the password is wrong.
func Verify(pw, encoded string) (ok bool, needsRehash bool, err error) {
	p, err := parsePHC(encoded)
	if err != nil {
		return false, false, err
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	default:
		return false, false, ErrBusy
	}
	got := argon2.IDKey([]byte(pw), p.salt, p.passes, p.memory, p.threads, uint32(len(p.key))) //nolint:gosec // parsePHC caps the key at maxKeyBytes
	if subtle.ConstantTimeCompare(got, p.key) != 1 {
		return false, false, nil
	}
	weaker := p.memory < memoryKiB || p.passes < passes || p.threads < threads ||
		len(p.salt) < saltLen || len(p.key) < keyLen
	return true, weaker, nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// VerifyDummy spends the same time as a real Verify and always fails. Call
// it for an unknown username so response time does not reveal which
// usernames exist. Its only error is ErrBusy.
func VerifyDummy(pw string) error {
	dummyOnce.Do(func() {
		// The fixed password is long enough, so Hash cannot fail on it.
		dummyHash, _ = Hash("dummy-password-for-timing")
	})
	_, _, err := Verify(pw, dummyHash)
	return err
}
