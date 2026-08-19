package crypto

import (
	"bytes"
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
