package crypto

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
)

func TestEncryptDecrypt_RoundTrip_ReturnsOriginalPlaintext(t *testing.T) {
	cases := []struct {
		name      string
		masterKey string
		plaintext []byte
	}{
		{"typical api key", "correct-master-key", []byte("dd-api-key-abc123")},
		{"empty plaintext", "correct-master-key", []byte("")},
		{"long plaintext", "correct-master-key", bytes.Repeat([]byte("x"), 4096)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := Encrypt(tc.masterKey, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() returned unexpected error: %v", err)
			}

			got, err := Decrypt(tc.masterKey, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() returned unexpected error: %v", err)
			}

			if !bytes.Equal(got, tc.plaintext) {
				t.Errorf("Decrypt(Encrypt(x)) = %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestDecrypt_WrongMasterKey_FailsLoudly_NeverReturnsCorruptedData(t *testing.T) {
	plaintext := []byte("dd-api-key-abc123")
	ciphertext, err := Encrypt("correct-master-key", plaintext)
	if err != nil {
		t.Fatalf("Encrypt() returned unexpected error: %v", err)
	}

	got, err := Decrypt("wrong-master-key", ciphertext)
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt() error = %v, want ErrDecryptionFailed", err)
	}
	if got != nil {
		t.Errorf("Decrypt() with wrong master key returned non-nil data %q, want nil (never corrupted data on failure)", got)
	}
}

func TestDecrypt_TamperedCiphertext_FailsLoudly(t *testing.T) {
	ciphertext, err := Encrypt("correct-master-key", []byte("dd-api-key-abc123"))
	if err != nil {
		t.Fatalf("Encrypt() returned unexpected error: %v", err)
	}

	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF

	got, err := Decrypt("correct-master-key", tampered)
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt() error = %v, want ErrDecryptionFailed", err)
	}
	if got != nil {
		t.Errorf("Decrypt() with tampered ciphertext returned non-nil data %q, want nil", got)
	}
}

func TestDecrypt_TruncatedCiphertext_FailsLoudly(t *testing.T) {
	_, err := Decrypt("correct-master-key", []byte("short"))
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt() error = %v, want ErrDecryptionFailed", err)
	}
}

// TestDeriveKey_NotPlainSHA256 is the M15 regression guard: deriveKey must
// go through PBKDF2's iteration stretching, not silently regress to a
// single unsalted sha256.Sum256 (which added no brute-force cost beyond the
// master key's own entropy).
func TestDeriveKey_NotPlainSHA256(t *testing.T) {
	const masterKey = "some-master-key-for-this-test"

	got := deriveKey(masterKey)
	plainSHA256 := sha256.Sum256([]byte(masterKey))

	if bytes.Equal(got, plainSHA256[:]) {
		t.Error("deriveKey() equals a plain sha256.Sum256(masterKey), want PBKDF2-stretched output")
	}
	if len(got) != 32 {
		t.Errorf("len(deriveKey()) = %d, want 32 (AES-256 key size)", len(got))
	}
}

// TestDeriveKey_Deterministic confirms deriveKey is a pure function of
// masterKey - Decrypt must be able to re-derive the exact same key Encrypt
// used, with no random or time-dependent input.
func TestDeriveKey_Deterministic(t *testing.T) {
	a := deriveKey("same-master-key")
	b := deriveKey("same-master-key")

	if !bytes.Equal(a, b) {
		t.Error("deriveKey() returned different output for the same masterKey across two calls, want deterministic")
	}
}
