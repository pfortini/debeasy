package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

type Keyring struct{ aead cipher.AEAD }

// NewKeyring derives an AES-256-GCM AEAD from the app secret via a domain-separated
// SHA-256 hash. The hash always yields 32 bytes, which aes.NewCipher and
// cipher.NewGCM accept without error, so this constructor is infallible.
func NewKeyring(appSecret []byte) *Keyring {
	// derive a 32-byte key independent of the cookie key by hashing with a domain tag
	h := sha256.Sum256(append([]byte("debeasy/keyring/v1\x00"), appSecret...))
	block, _ := aes.NewCipher(h[:]) // infallible: len(key) == 32
	aead, _ := cipher.NewGCM(block) // infallible: block is a valid AES cipher
	return &Keyring{aead: aead}
}

func (k *Keyring) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *Keyring) Open(ciphertext []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return k.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// CookieKeys derives two 32-byte keys (hash, block) from the app secret for gorilla/securecookie.
func CookieKeys(appSecret []byte) (hashKey, blockKey []byte) {
	h := sha256.Sum256(append([]byte("debeasy/cookie-hash/v1\x00"), appSecret...))
	b := sha256.Sum256(append([]byte("debeasy/cookie-block/v1\x00"), appSecret...))
	return h[:], b[:]
}
