package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordAndVerify(t *testing.T) {
	password := "correct-horse-battery-staple"

	stored, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}

	if !strings.Contains(stored, ":") {
		t.Fatalf("expected stored hash to contain ':', got %s", stored)
	}

	if !verifyPassword(stored, password) {
		t.Error("verifyPassword should succeed with matching password")
	}

	if verifyPassword(stored, "wrong-password") {
		t.Error("verifyPassword should fail with wrong password")
	}
}

func TestSplitStored_Valid(t *testing.T) {
	stored, err := hashPassword("any-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}

	salt, hash, ok := splitStored(stored)
	if !ok {
		t.Fatal("splitStored returned false for valid stored hash")
	}
	if len(salt) != saltLen {
		t.Errorf("expected salt length %d, got %d", saltLen, len(salt))
	}
	if len(hash) != argon2KeyLen {
		t.Errorf("expected hash length %d, got %d", argon2KeyLen, len(hash))
	}
}

func TestSplitStored_Invalid(t *testing.T) {
	cases := []string{
		"",
		"nocolon",
		"gg:zz",
		"aa:aa",
	}

	for _, c := range cases {
		_, _, ok := splitStored(c)
		if ok {
			t.Errorf("splitStored should reject %q", c)
		}
	}
}

func TestVerifyPassword_MalformedStored(t *testing.T) {
	if verifyPassword("not-a-hash", "password") {
		t.Error("verifyPassword should reject malformed stored hash")
	}
}
