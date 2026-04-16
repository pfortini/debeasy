package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newSecret(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func TestKeyring_SealOpen_Roundtrip(t *testing.T) {
	k := NewKeyring(newSecret(t))
	cases := [][]byte{
		[]byte(""),
		[]byte("hello"),
		[]byte("The quick brown fox jumps over the lazy dog."),
		bytes.Repeat([]byte{0xab}, 4096),
	}
	for _, pt := range cases {
		ct, err := k.Seal(pt)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		got, err := k.Open(ct)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("roundtrip mismatch: got %q want %q", got, pt)
		}
	}
}

func TestKeyring_Seal_FreshNonce(t *testing.T) {
	k := NewKeyring(newSecret(t))
	pt := []byte("same plaintext")
	a, _ := k.Seal(pt)
	b, _ := k.Seal(pt)
	if bytes.Equal(a, b) {
		t.Fatalf("two Seals of same plaintext produced identical ciphertext — nonce isn't fresh")
	}
}

func TestKeyring_Open_TamperDetected(t *testing.T) {
	k := NewKeyring(newSecret(t))
	ct, _ := k.Seal([]byte("secret"))
	ct[len(ct)-1] ^= 0x01 // flip last byte
	if _, err := k.Open(ct); err == nil {
		t.Fatalf("tampered ciphertext should fail Open")
	}
}

func TestKeyring_Open_Short(t *testing.T) {
	k := NewKeyring(newSecret(t))
	if _, err := k.Open([]byte{0x00}); err == nil {
		t.Fatalf("short ciphertext should error")
	}
}

func TestKeyring_WrongKey(t *testing.T) {
	a := NewKeyring(newSecret(t))
	b := NewKeyring(newSecret(t))
	ct, _ := a.Seal([]byte("hi"))
	if _, err := b.Open(ct); err == nil {
		t.Fatalf("wrong key should not Open")
	}
}

func TestCookieKeys_Deterministic(t *testing.T) {
	secret := newSecret(t)
	h1, b1 := CookieKeys(secret)
	h2, b2 := CookieKeys(secret)
	if !bytes.Equal(h1, h2) || !bytes.Equal(b1, b2) {
		t.Fatalf("CookieKeys should be deterministic for same secret")
	}
	if len(h1) != 32 || len(b1) != 32 {
		t.Fatalf("CookieKeys should return 32-byte keys; got %d/%d", len(h1), len(b1))
	}
	if bytes.Equal(h1, b1) {
		t.Fatalf("hash and block keys should differ")
	}
}

func TestCookieKeys_DifferentSecrets(t *testing.T) {
	h1, _ := CookieKeys(newSecret(t))
	h2, _ := CookieKeys(newSecret(t))
	if bytes.Equal(h1, h2) {
		t.Fatalf("different secrets must produce different cookie keys")
	}
}
