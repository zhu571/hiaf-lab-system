package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func withRequestID(r *http.Request) *http.Request {
	return r.WithContext(common.SetRequestID(r.Context(), "test-id"))
}

func TestServiceTokenSuccess(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("secret-token")

	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsServiceCall(r.Context()) {
			t.Fatal("expected service call context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id=u1", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenWrongToken(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("secret-token")

	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run on invalid token")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestServiceTokenNonWhitelistPath(t *testing.T) {
	// 白名单外的路径不消费 token，原样放行（无 service call 标记）。
	SetServiceToken("secret-token")
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("non-whitelist path must not be marked as service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/todos", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenWrongMethod(t *testing.T) {
	// 白名单仅 GET：POST 到 by-date 不放行（无 service call 标记，交由 JWT 流程）。
	SetServiceToken("secret-token")
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("POST must not be a service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenAuthRequiredBypass(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("secret-token")

	// service call 在 AuthRequired 之前 → 直接放行
	stack := ServiceToken()(AuthRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsServiceCall(r.Context()) {
			t.Fatal("expected service call")
		}
		if GetUserClaims(r.Context()) != nil {
			t.Fatal("service call must not have user claims")
		}
		w.WriteHeader(http.StatusOK)
	})))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	stack.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenMissingTokenNoMark(t *testing.T) {
	// 白名单路径但无 Authorization → 放行交给 AuthRequired（其返回 401）。
	SetServiceToken("secret-token")
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("no token must not be a service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenJWTPassthrough(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("svc-secret")
	SetJWTSecret([]byte("test-secret-32-bytes-long!!!!!"))

	// 前端 by-date 带普通 JWT（3 段点分）→ 不按 service token 拦截，交给 AuthRequired
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("JWT must not be treated as service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyX2lkIjoidTEifQ.sig")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("JWT passthrough expected 200, got %d", w.Code)
	}

	// 完整链路：JWT 用户调 by-date → AuthRequired 通过（无 service call 标记）
	stack := ServiceToken()(AuthRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetUserClaims(r.Context())
		if claims == nil || claims.UserID != "u1" {
			t.Fatalf("expected u1 claims, got %+v", claims)
		}
		w.WriteHeader(http.StatusOK)
	})))
	token, err := GenerateToken("u1", "alice", "member", 1, []byte("test-secret-32-bytes-long!!!!!"))
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	stack.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("JWT full chain expected 200, got %d", w.Code)
	}
}

func TestServiceTokenNonBearerPassthrough(t *testing.T) {
	// 非 Bearer 形态（如 Basic）不当 service token 消费，原样放行（不标 service call）。
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("svc-secret")
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("non-Bearer header must not be marked as service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLooksLikeJWT(t *testing.T) {
	if !looksLikeJWT("eyJhbGciOiJIUzI1NiJ9.abc.def") {
		t.Fatal("3-segment token should look like JWT")
	}
	if looksLikeJWT("0123456789abcdef") {
		t.Fatal("hex service token must not look like JWT")
	}
	if looksLikeJWT("") || looksLikeJWT("a.b") {
		t.Fatal("malformed tokens must not look like JWT")
	}
}

func TestServiceTokenAskExecute(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("secret-token")

	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsServiceCall(r.Context()) {
			t.Fatal("expected service call context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceTokenAskExecuteWrongToken(t *testing.T) {
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("secret-token")

	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run on invalid token")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestServiceTokenAskExecuteWrongMethod(t *testing.T) {
	// ask/execute 白名单仅 POST：GET 不放行（无 service call 标记，交由 JWT 流程）。
	SetServiceToken("secret-token")
	handler := ServiceToken()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsServiceCall(r.Context()) {
			t.Fatal("GET ask/execute must not be a service call")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ask/execute", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(w, withRequestID(r))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
