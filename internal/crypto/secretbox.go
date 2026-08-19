// Package crypto encrypts and decrypts secrets vane stores at rest (e.g. the
// Datadog API key), using the operator-supplied VANE_MASTER_KEY.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// ErrDecryptionFailed is returned when ciphertext cannot be authenticated
// against masterKey - either the key is wrong or the ciphertext was
// tampered with. It never returns partially-decrypted or corrupted data:
// GCM's authentication tag check fails closed.
var ErrDecryptionFailed = errors.New("crypto: decryption failed")

// deriveKey stretches masterKey (an arbitrary-length operator-supplied
// string) into the fixed 32-byte key AES-256 requires.
func deriveKey(masterKey string) []byte {
	sum := sha256.Sum256([]byte(masterKey))
	return sum[:]
}

// Encrypt seals plaintext with AES-256-GCM under masterKey, returning a
// single blob (random nonce prepended to the ciphertext) safe to persist.
func Encrypt(masterKey string, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(masterKey)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt. It returns ErrDecryptionFailed if masterKey is
// wrong or the ciphertext is malformed/tampered - never corrupted plaintext.
func Decrypt(masterKey string, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(masterKey)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, ErrDecryptionFailed
	}

	nonce, sealed := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

func newGCM(masterKey string) (cipher.AEAD, error) {
	block, err := aes.NewCipher(deriveKey(masterKey))
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM mode: %w", err)
	}

	return gcm, nil
}
