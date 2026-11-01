package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format %q", h)
	}
	if err := VerifyPassword("correct horse battery", h); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := VerifyPassword("wrong", h); err != ErrMismatch {
		t.Fatalf("want ErrMismatch, got %v", err)
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
	if VerifyPassword("x", "$2a$10$bcrypt") == nil || VerifyPassword("x", "garbage") == nil {
		t.Fatal("malformed hashes must not verify")
	}
}
