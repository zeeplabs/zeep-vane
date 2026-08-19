package auth

import "testing"

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
