package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// weakHash builds a valid PHC string with the given settings.
func weakHash(pw string, m, t uint32, p uint8, saltSize int, key uint32) string {
	salt := make([]byte, saltSize)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	k := argon2.IDKey([]byte(pw), salt, t, m, p, key)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", m, t, p, b64.EncodeToString(salt), b64.EncodeToString(k))
}

func TestHashAndVerify(t *testing.T) {
	const pw = "correct horse battery"
	h, err := Hash(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected format: %s", h)
	}
	if strings.Contains(h, pw) {
		t.Fatal("hash contains the password")
	}
	ok, rehash, err := Verify(pw, h)
	if err != nil || !ok || rehash {
		t.Fatalf("right password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	ok, rehash, err = Verify("wrong horse battery", h)
	if err != nil || ok || rehash {
		t.Fatalf("wrong password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	h2, _ := Hash(pw)
	if h == h2 {
		t.Fatal("two hashes of one password are identical, so the salt is not random")
	}
}

func TestVerifyRehashDetection(t *testing.T) {
	const pw = "correct horse battery"
	cases := []struct {
		name       string
		encoded    string
		wantRehash bool
	}{
		{"current settings", weakHash(pw, 19456, 2, 1, 16, 32), false},
		{"stronger settings", weakHash(pw, 32768, 3, 2, 16, 32), false},
		{"less memory", weakHash(pw, 8192, 2, 1, 16, 32), true},
		{"fewer passes", weakHash(pw, 19456, 1, 1, 16, 32), true},
		{"short salt", weakHash(pw, 19456, 2, 1, 8, 32), true},
		{"short key", weakHash(pw, 19456, 2, 1, 16, 16), true},
	}
	for _, c := range cases {
		ok, rehash, err := Verify(pw, c.encoded)
		if err != nil || !ok {
			t.Errorf("%s: ok=%v err=%v", c.name, ok, err)
		}
		if rehash != c.wantRehash {
			t.Errorf("%s: needsRehash=%v, want %v", c.name, rehash, c.wantRehash)
		}
	}
	// A wrong password never asks for a rehash, even on old settings.
	if ok, rehash, _ := Verify("not the password", weakHash(pw, 8192, 1, 1, 16, 32)); ok || rehash {
		t.Errorf("wrong password on weak hash: ok=%v rehash=%v", ok, rehash)
	}
}

func TestVerifyRejectsBadStoredHash(t *testing.T) {
	good := weakHash("x", 19456, 2, 1, 16, 32)
	parts := strings.Split(good, "$")
	cases := map[string]string{
		"empty":           "",
		"bcrypt":          "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012345",
		"argon2i":         strings.Replace(good, "argon2id", "argon2i", 1),
		"old version":     strings.Replace(good, "v=19", "v=16", 1),
		"missing field":   "$argon2id$v=19$m=19456,t=2$" + parts[4] + "$" + parts[5],
		"huge memory":     strings.Replace(good, "m=19456", "m=4294967295", 1),
		"zero passes":     strings.Replace(good, "t=2", "t=0", 1),
		"too many passes": strings.Replace(good, "t=2", "t=999", 1),
		"bad salt":        "$argon2id$v=19$m=19456,t=2,p=1$!!!$" + parts[5],
		"empty key":       "$argon2id$v=19$m=19456,t=2,p=1$" + parts[4] + "$",
		"extra segment":   good + "$more",
	}
	for name, enc := range cases {
		ok, rehash, err := Verify("x", enc)
		if err == nil || ok || rehash {
			t.Errorf("%s: ok=%v rehash=%v err=%v", name, ok, rehash, err)
		}
	}
}

func TestCheckPassword(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"empty", "", false},
		{"eleven characters", strings.Repeat("a", 11), false},
		{"twelve characters", strings.Repeat("a", 12), true},
		{"twelve multi-byte characters", strings.Repeat("é", 12), true},
		{"eleven multi-byte characters", strings.Repeat("é", 11), false},
		{"1024 bytes", strings.Repeat("a", 1024), true},
		{"1025 bytes", strings.Repeat("a", 1025), false},
	}
	for _, c := range cases {
		err := CheckPassword(c.pw)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrPasswordLength) {
			t.Errorf("%s: err is not ErrPasswordLength", c.name)
		}
		if _, herr := Hash(c.pw); (herr == nil) != c.ok {
			t.Errorf("%s: Hash err=%v, want ok=%v", c.name, herr, c.ok)
		}
	}
}

func TestVerifyDummyDoesNotPanic(t *testing.T) {
	_ = VerifyDummy("anything at all")
	_ = VerifyDummy("")
}

// At most maxHashing checks run at once; the next is refused, not queued.
func TestVerifyRefusesWhenAllSlotsAreTaken(t *testing.T) {
	hash, err := Hash("a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	for range maxHashing {
		hashSlots <- struct{}{}
	}
	if _, _, err := Verify("a long enough password", hash); !errors.Is(err, ErrBusy) {
		t.Errorf("Verify: got %v, want ErrBusy", err)
	}
	if err := VerifyDummy("x"); !errors.Is(err, ErrBusy) {
		t.Errorf("VerifyDummy: got %v, want ErrBusy", err)
	}
	for range maxHashing {
		<-hashSlots
	}
	if ok, _, err := Verify("a long enough password", hash); !ok || err != nil {
		t.Errorf("after slots freed: ok=%v err=%v", ok, err)
	}
}
