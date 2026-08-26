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

	"golang.org/x/crypto/pbkdf2"
)

// ErrDecryptionFailed is returned when ciphertext cannot be authenticated
// against masterKey - either the key is wrong or the ciphertext was
// tampered with. It never returns partially-decrypted or corrupted data:
// GCM's authentication tag check fails closed.
var ErrDecryptionFailed = errors.New("crypto: decryption failed")

// pbkdf2Iterations follows OWASP's PBKDF2-HMAC-SHA256 guidance (>=210,000 as
// of the 2023 cheat sheet). VANE_MASTER_KEY only needs to be >=32 characters
// (config.requireSecret) - that says nothing about its actual entropy for an
// operator-chosen string, so the real defense against a weak-but-long master
// key is making each guess expensive to try, not merely storing the key
// differently (M15).
const pbkdf2Iterations = 210_000

// pbkdf2Salt is a fixed, public domain-separation constant, not a secret -
// there is nowhere to persist a random per-installation salt without a
// migration (every already-encrypted Datadog credential would become
// undecryptable), and PBKDF2's cost-hardening against a weak or guessed
// master key comes from the iteration count, not from the salt being
// unknown to an attacker who already has the source code.
var pbkdf2Salt = []byte("zeep-vane/master-key/v1")

// deriveKey stretches masterKey (an arbitrary-length operator-supplied
// string) into the fixed 32-byte key AES-256 requires, via PBKDF2-HMAC-SHA256
// (M15) - previously a single unsalted sha256.Sum256, which added no
// brute-force cost at all beyond the master key's own entropy.
func deriveKey(masterKey string) []byte {
	return pbkdf2.Key([]byte(masterKey), pbkdf2Salt, pbkdf2Iterations, 32, sha256.New)
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
