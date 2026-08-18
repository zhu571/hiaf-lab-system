package auth

import (
	"errors"
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

func TestRemovesActiveAdmin(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name string
		req  AdminUpdateUserRequest
		want bool
	}{
		{"disable true triggers", AdminUpdateUserRequest{Disabled: boolPtr(true)}, true},
		{"disable false does not", AdminUpdateUserRequest{Disabled: boolPtr(false)}, false},
		{"role demotion triggers", AdminUpdateUserRequest{Role: strPtr(RoleMember)}, true},
		{"role admin does not", AdminUpdateUserRequest{Role: strPtr(RoleAdmin)}, false},
		{"display name only does not", AdminUpdateUserRequest{DisplayName: strPtr("x")}, false},
		{"empty request does not", AdminUpdateUserRequest{}, false},
	}

	for _, c := range cases {
		if got := removesActiveAdmin(c.req); got != c.want {
			t.Errorf("%s: removesActiveAdmin = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidLanguage(t *testing.T) {
	for _, lang := range []string{LanguageZH, LanguageEN} {
		if !validLanguage(lang) {
			t.Errorf("validLanguage(%q) should be true", lang)
		}
	}
	for _, lang := range []string{"", "fr", "zh-CN", "english"} {
		if validLanguage(lang) {
			t.Errorf("validLanguage(%q) should be false", lang)
		}
	}
}

// D12：强密码校验器——长度≥10 且同时含字母和数字。
func TestValidatePassword(t *testing.T) {
	weak := []string{"", "short", "abcdefghij", "ABCDEFGHIJ", "1234567890", "abcdefgh9", "密码密码密码密码"}
	for _, pw := range weak {
		if validatePassword(pw) {
			t.Errorf("validatePassword(%q) = true, want false", pw)
		}
	}
	strong := []string{"password123", "1234567890a", "Passw0rd!!", "abcd1234EF", "密码abc12345"}
	for _, pw := range strong {
		if !validatePassword(pw) {
			t.Errorf("validatePassword(%q) = false, want true", pw)
		}
	}
}

// D12：弱密码在注册/管理员开号/改密三处入口均被拒（校验先于 repo 访问，nil repo 即可验证）。
func TestWeakPasswordRejectedAtService(t *testing.T) {
	svc := NewService(nil, nil)
	if _, err := svc.Register("user1", "weak1"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("Register weak password: got %v, want ErrPasswordTooShort", err)
	}
	if err := svc.ChangePassword("uid", "old", "weak1"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("ChangePassword weak password: got %v, want ErrPasswordTooShort", err)
	}
	if _, _, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "user2", Password: "weak1"}); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("AdminCreateUser weak password: got %v, want ErrPasswordTooShort", err)
	}
}

func TestRegisterRequiresInvitationWhenProvided(t *testing.T) {
	svc := NewService(nil, nil)
	if _, err := svc.Register("user1", "StrongPass123", " "); !errors.Is(err, ErrInvitationRequired) {
		t.Fatalf("empty invitation: got %v, want ErrInvitationRequired", err)
	}
}
