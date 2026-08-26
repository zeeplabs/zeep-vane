package auth

import (
	"strings"
	"testing"
)

func TestValidatePassword_MinimumLength_Accepted(t *testing.T) {
	if err := ValidatePassword("8-chars!"); err != nil {
		t.Errorf("ValidatePassword() returned unexpected error for an 8-character password: %v", err)
	}
}

func TestValidatePassword_TooShort_Rejected(t *testing.T) {
	if err := ValidatePassword("1234567"); err == nil {
		t.Error("ValidatePassword() = nil for a 7-character password, want an error")
	}
}

func TestValidatePassword_Empty_Rejected(t *testing.T) {
	if err := ValidatePassword(""); err == nil {
		t.Error("ValidatePassword() = nil for an empty password, want an error")
	}
}

func TestValidatePassword_MaximumLength_Accepted(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength)); err != nil {
		t.Errorf("ValidatePassword() returned unexpected error for a %d-character password: %v", MaxPasswordLength, err)
	}
}

func TestValidatePassword_TooLong_Rejected(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Errorf("ValidatePassword() = nil for a %d-character password, want an error (bcrypt's own hard limit)", MaxPasswordLength+1)
	}
}

func TestHashPassword_NeverEqualsPlaintext(t *testing.T) {
	const plain = "correct-horse-battery-staple"

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}

	if hash == plain {
		t.Error("HashPassword() returned the plaintext unchanged, want a hash")
	}
}

func TestVerifyPassword_CorrectPassword_True(t *testing.T) {
	const plain = "correct-horse-battery-staple"

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}

	if !VerifyPassword(hash, plain) {
		t.Error("VerifyPassword() = false for the correct password, want true")
	}
}

func TestVerifyPassword_IncorrectPassword_False(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}

	if VerifyPassword(hash, "wrong-password") {
		t.Error("VerifyPassword() = true for an incorrect password, want false")
	}
}
