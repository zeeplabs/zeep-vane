package tls

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/caddyserver/certmagic"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

// secretKeySuffix identifies the CertMagic artifacts that hold private key
// material: a certificate's domain key and the ACME account key both end in
// ".key". Public artifacts (.crt, .json) deliberately do not.
const secretKeySuffix = ".key"

// secretEnvelopePrefix is the versioned, self-describing marker prepended to
// a sealed value (AD-030). A legacy plaintext private key is a PEM and starts
// with "-----BEGIN", so it can never carry this marker by accident.
var secretEnvelopePrefix = []byte("vane:tls-secret:v1:")

// EncryptedStorage decorates a certmagic.Storage so private key material is
// sealed at rest with the operator's master key (AD-030). Only values whose
// key ends in ".key" are encrypted; every other value and every other storage
// method pass through unchanged, so CertMagic sees identical behavior.
type EncryptedStorage struct {
	inner     certmagic.Storage
	masterKey string
}

var _ certmagic.Storage = (*EncryptedStorage)(nil)

// NewEncryptedStorage wraps inner, sealing private keys with masterKey.
func NewEncryptedStorage(inner certmagic.Storage, masterKey string) *EncryptedStorage {
	return &EncryptedStorage{inner: inner, masterKey: masterKey}
}

// Store seals a ".key" value under the envelope before delegating; any other
// key is stored verbatim.
func (s *EncryptedStorage) Store(ctx context.Context, key string, value []byte) error {
	if !isSecretKey(key) {
		return s.inner.Store(ctx, key, value)
	}
	sealed, err := sealSecret(s.masterKey, value)
	if err != nil {
		return fmt.Errorf("tls: failed to seal %s: %w", key, err)
	}
	return s.inner.Store(ctx, key, sealed)
}

// Load opens a sealed ".key" value. A ".key" without the envelope marker is a
// legacy plaintext key and is returned unchanged; so is every non-".key"
// value. A sealed value that fails to decrypt returns an explicit error that
// is not fs.ErrNotExist, so CertMagic never mistakes a wrong master key for a
// missing key and reissues.
func (s *EncryptedStorage) Load(ctx context.Context, key string) ([]byte, error) {
	value, err := s.inner.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	if !isSecretKey(key) || !isSealed(value) {
		return value, nil
	}
	plaintext, err := crypto.Decrypt(s.masterKey, value[len(secretEnvelopePrefix):])
	if err != nil {
		return nil, fmt.Errorf("tls: failed to decrypt %s (wrong VANE_MASTER_KEY?): %w", key, err)
	}
	return plaintext, nil
}

func (s *EncryptedStorage) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, key)
}

func (s *EncryptedStorage) Exists(ctx context.Context, key string) bool {
	return s.inner.Exists(ctx, key)
}

func (s *EncryptedStorage) List(ctx context.Context, path string, recursive bool) ([]string, error) {
	return s.inner.List(ctx, path, recursive)
}

func (s *EncryptedStorage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {
	return s.inner.Stat(ctx, key)
}

func (s *EncryptedStorage) Lock(ctx context.Context, name string) error {
	return s.inner.Lock(ctx, name)
}

func (s *EncryptedStorage) Unlock(ctx context.Context, name string) error {
	return s.inner.Unlock(ctx, name)
}

func isSecretKey(key string) bool {
	return strings.HasSuffix(key, secretKeySuffix)
}

func isSealed(value []byte) bool {
	return bytes.HasPrefix(value, secretEnvelopePrefix)
}

func sealSecret(masterKey string, plaintext []byte) ([]byte, error) {
	sealed, err := crypto.Encrypt(masterKey, plaintext)
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, secretEnvelopePrefix...), sealed...), nil
}
