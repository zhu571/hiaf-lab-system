package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type userClaimsKeyType string

const userClaimsKey userClaimsKeyType = "user_claims"

// UserClaims carries authenticated user identity inside a JWT.
type UserClaims struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

// JWTSecret holds the HMAC secret used to sign and verify tokens.
// It must be initialized before AuthRequired is used.
var JWTSecret []byte

// TokenVersionValidator, when set, is called by AuthRequired to ensure the
// token's token_version still matches the user's current version. This lets
// password changes invalidate previously issued access tokens.
var TokenVersionValidator func(userID string, version int) bool

// SetJWTSecret sets the global JWT signing secret.
func SetJWTSecret(secret []byte) {
	JWTSecret = secret
}

// AuthRequired validates the Bearer token and injects UserClaims into the request context.
func AuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ""
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			tokenStr = strings.TrimPrefix(header, "Bearer ")
		} else if cookie, err := r.Cookie("access_token"); err == nil {
			tokenStr = cookie.Value
		}
		if tokenStr == "" {
			common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "缺少认证凭据", nil)
			return
		}
		claims := &UserClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return JWTSecret, nil
		})

		if err != nil || !token.Valid {
			common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "token 无效或已过期", nil)
			return
		}

		if TokenVersionValidator != nil && !TokenVersionValidator(claims.UserID, claims.TokenVersion) {
			common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "token 已失效", nil)
			return
		}

		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserClaims extracts UserClaims from the request context.
func GetUserClaims(ctx context.Context) *UserClaims {
	claims, _ := ctx.Value(userClaimsKey).(*UserClaims)
	return claims
}

// GenerateToken creates a short-lived access token valid for 15 minutes.
func GenerateToken(userID, username, role string, tokenVersion int, secret []byte) (string, error) {
	claims := &UserClaims{
		UserID:       userID,
		Username:     username,
		Role:         role,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}
