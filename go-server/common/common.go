package common

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// SetRequestID attaches a request ID to the context.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// GetRequestID returns the request ID stored in the context, or an empty string.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// SetRequestIDHeader 把 ctx 中的 request_id 透传到下游服务（如 py-agent），
// 使一次问答可在 Go / Python / 模型日志中用同一个 ID 关联（AR-07）。
func SetRequestIDHeader(ctx context.Context, req *http.Request) {
	if id := GetRequestID(ctx); id != "" {
		req.Header.Set("X-Request-Id", id)
	}
}

// ReadSecret reads a secret from a Docker secret file and falls back to an environment variable.
func ReadSecret(filePath, envKey string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if v := os.Getenv(envKey); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("secret not found at %s and env %s not set", filePath, envKey)
}
