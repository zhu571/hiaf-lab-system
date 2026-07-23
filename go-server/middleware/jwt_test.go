package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func TestAuthRequired_MissingHeader(t *testing.T) {
	handler := AuthRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := common.SetRequestID(r.Context(), "test-id")
	r = r.WithContext(ctx)
	handler.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long!!!!!")
	SetJWTSecret(secret)

	token, err := GenerateToken("user-1", "testuser", "member", 1, secret)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	handler := AuthRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetUserClaims(r.Context())
		if claims == nil {
			t.Fatal("expected non-nil claims")
		}
		if claims.Username != "testuser" {
			t.Errorf("expected username testuser, got %s", claims.Username)
		}
		if claims.Role != "member" {
			t.Errorf("expected role member, got %s", claims.Role)
		}
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := common.SetRequestID(r.Context(), "test-id")
	r = r.WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
