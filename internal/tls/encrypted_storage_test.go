package tls

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/caddyserver/certmagic"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

const testMasterKey = "0123456789abcdef0123456789abcdef"

const (
	keyPath     = "certificates/acme-v02.api.letsencrypt.org-directory/status.example/status.example.key"
	crtPath     = "certificates/acme-v02.api.letsencrypt.org-directory/status.example/status.example.crt"
	acmeKeyPath = "acme/acme-v02.api.letsencrypt.org-directory/users/ops@example.com/ops@example.com.key"
)

// fakeStorage is an in-memory certmagic.Storage that records exactly what the
// decorator hands to the wrapped storage, with no transformation of its own.
type fakeStorage struct {
	mu    sync.Mutex
	data  map[string][]byte
	locks []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: make(map[string][]byte)}
}

func (f *fakeStorage) Store(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeStorage) Load(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; !ok {
		return fs.ErrNotExist
	}
	delete(f.data, key)
	return nil
}

func (f *fakeStorage) Exists(_ context.Context, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[key]
	return ok
}

func (f *fakeStorage) List(_ context.Context, prefix string, _ bool) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeStorage) Stat(_ context.Context, key string) (certmagic.KeyInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return certmagic.KeyInfo{}, fs.ErrNotExist
	}
	return certmagic.KeyInfo{Key: key, Size: int64(len(v)), IsTerminal: true}, nil
}

func (f *fakeStorage) Lock(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locks = append(f.locks, "lock:"+name)
	return nil
}

func (f *fakeStorage) Unlock(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locks = append(f.locks, "unlock:"+name)
	return nil
}

func pemPrivateKey(body string) []byte {
	return []byte("-----BEGIN PRIVATE KEY-----\n" + body + "\n-----END PRIVATE KEY-----\n")
}

// TLSKEY-01, TLSKEY-04: a .key value is persisted sealed, under the versioned
// envelope marker, never as plaintext.
func TestEncryptedStorage_StoreSealsPrivateKey(t *testing.T) {
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	plaintext := pemPrivateKey("super-secret-key-material")

	if err := s.Store(context.Background(), keyPath, plaintext); err != nil {
		t.Fatalf("Store: %v", err)
	}

	raw, err := inner.Load(context.Background(), keyPath)
	if err != nil {
		t.Fatalf("inner Load: %v", err)
	}
	if !bytes.HasPrefix(raw, secretEnvelopePrefix) {
		t.Fatalf("stored value missing envelope prefix, got %q", raw)
	}
	if bytes.Contains(raw, []byte("super-secret-key-material")) {
		t.Fatalf("stored value still contains plaintext: %q", raw)
	}
	if bytes.Equal(raw, plaintext) {
		t.Fatal("stored value equals plaintext")
	}
}

// TLSKEY-02: a sealed .key value is decrypted back to its original bytes.
func TestEncryptedStorage_LoadRoundTrip(t *testing.T) {
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	plaintext := pemPrivateKey("round-trip-material")

	if err := s.Store(context.Background(), keyPath, plaintext); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := s.Load(context.Background(), keyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Load returned %q, want %q", got, plaintext)
	}
}

// TLSKEY-03: non-.key values are stored and returned byte-for-byte unchanged.
func TestEncryptedStorage_NonSecretValuesUntouched(t *testing.T) {
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	crt := []byte("-----BEGIN CERTIFICATE-----\npublic-material\n-----END CERTIFICATE-----\n")
	meta := []byte(`{"sans":["status.example"],"issuer":"le"}`)

	for _, tc := range []struct {
		key   string
		value []byte
	}{
		{crtPath, crt},
		{strings.TrimSuffix(crtPath, ".crt") + ".json", meta},
	} {
		if err := s.Store(context.Background(), tc.key, tc.value); err != nil {
			t.Fatalf("Store(%s): %v", tc.key, err)
		}
		raw, err := inner.Load(context.Background(), tc.key)
		if err != nil {
			t.Fatalf("inner Load(%s): %v", tc.key, err)
		}
		if !bytes.Equal(raw, tc.value) {
			t.Fatalf("inner value for %s changed: %q", tc.key, raw)
		}
		got, err := s.Load(context.Background(), tc.key)
		if err != nil {
			t.Fatalf("Load(%s): %v", tc.key, err)
		}
		if !bytes.Equal(got, tc.value) {
			t.Fatalf("Load(%s) = %q, want %q", tc.key, got, tc.value)
		}
	}
}

// TLSKEY-06: a legacy plaintext .key (no marker) is returned as-is, so an
// upgrade cannot break certificates issued before encryption existed.
func TestEncryptedStorage_LegacyPlaintextKeyPassThrough(t *testing.T) {
	inner := newFakeStorage()
	if err := inner.Store(context.Background(), keyPath, pemPrivateKey("legacy")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewEncryptedStorage(inner, testMasterKey)

	got, err := s.Load(context.Background(), keyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := pemPrivateKey("legacy"); !bytes.Equal(got, want) {
		t.Fatalf("Load = %q, want legacy plaintext %q", got, want)
	}
}

// TLSKEY-05: a sealed .key that will not decrypt with the current master key
// fails explicitly and is never reported as missing.
func TestEncryptedStorage_WrongMasterKeyFailsExplicitly(t *testing.T) {
	inner := newFakeStorage()
	writer := NewEncryptedStorage(inner, "key-A-012345678901234567890123456")
	if err := writer.Store(context.Background(), keyPath, pemPrivateKey("material")); err != nil {
		t.Fatalf("Store: %v", err)
	}

	reader := NewEncryptedStorage(inner, "key-B-012345678901234567890123456")
	_, err := reader.Load(context.Background(), keyPath)
	if !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("Load error = %v, want crypto.ErrDecryptionFailed", err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("decrypt failure must not be reported as fs.ErrNotExist (would trigger reissue)")
	}
}

// TLSKEY-09: every method other than Store/Load reaches the inner storage.
func TestEncryptedStorage_DelegatesNonCryptographicMethods(t *testing.T) {
	ctx := context.Background()
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	if err := s.Store(ctx, keyPath, pemPrivateKey("material")); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if !s.Exists(ctx, keyPath) {
		t.Fatal("Exists did not reach the inner storage")
	}
	keys, err := s.List(ctx, "certificates/", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 || keys[0] != keyPath {
		t.Fatalf("List = %v, want [%s]", keys, keyPath)
	}
	if err := s.Lock(ctx, "cert-lock"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := s.Unlock(ctx, "cert-lock"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if len(inner.locks) != 2 || inner.locks[0] != "lock:cert-lock" || inner.locks[1] != "unlock:cert-lock" {
		t.Fatalf("Lock/Unlock did not reach the inner storage: %v", inner.locks)
	}
	if err := s.Delete(ctx, keyPath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Exists(ctx, keyPath) {
		t.Fatal("Delete did not remove the inner value")
	}
}

// TLSKEY-03: Stat on a .key reports the sealed size; it must not decrypt.
func TestEncryptedStorage_StatOnSecretKeyDoesNotDecrypt(t *testing.T) {
	ctx := context.Background()
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	plaintext := pemPrivateKey("material")
	if err := s.Store(ctx, keyPath, plaintext); err != nil {
		t.Fatalf("Store: %v", err)
	}

	raw, err := inner.Load(ctx, keyPath)
	if err != nil {
		t.Fatalf("inner Load: %v", err)
	}
	info, err := s.Stat(ctx, keyPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsTerminal {
		t.Fatal("Stat reported a non-terminal key")
	}
	if info.Size != int64(len(raw)) {
		t.Fatalf("Stat size = %d, want sealed size %d (Stat must not decrypt)", info.Size, len(raw))
	}
}

// TLSKEY-03: a non-.key value that happens to contain the marker text is
// still passed through untouched.
func TestEncryptedStorage_NonSecretValueContainingMarkerUntouched(t *testing.T) {
	ctx := context.Background()
	inner := newFakeStorage()
	s := NewEncryptedStorage(inner, testMasterKey)
	value := append(append([]byte{}, secretEnvelopePrefix...), []byte("not-a-key")...)
	path := "acme/acme-v02.api.letsencrypt.org-directory/users/ops@example.com/ops@example.com.json"

	if err := s.Store(ctx, path, value); err != nil {
		t.Fatalf("Store: %v", err)
	}
	raw, err := inner.Load(ctx, path)
	if err != nil {
		t.Fatalf("inner Load: %v", err)
	}
	if !bytes.Equal(raw, value) {
		t.Fatalf("non-.key value was transformed: %q", raw)
	}
	got, err := s.Load(ctx, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("Load = %q, want %q", got, value)
	}
}

// TLSKEY-01: the secret predicate matches CertMagic's real key shapes.
func TestIsSecretKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{keyPath, true},
		{acmeKeyPath, true},
		{crtPath, false},
		{"certificates/acme-v02.example/status.example/status.example.json", false},
		{"acme/acme-v02.example/users/ops@example.com/ops@example.com.json", false},
		{"certificates/acme-v02.example/status.example/status.example", false},
	}
	for _, tc := range cases {
		if got := isSecretKey(tc.key); got != tc.want {
			t.Errorf("isSecretKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
