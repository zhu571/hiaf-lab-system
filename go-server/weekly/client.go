package weekly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// httpLLMClient 生产 LLM 客户端：调 py-agent /v1/weekly-summary（360s 超时——
// 两步 LLM 预算 300s + 余量；对齐 todos httpLLMPlanner 的 AutoConfigure 鉴权模式）。
type httpLLMClient struct {
	client *http.Client
	url    string
	token  string
}

func NewHTTPLLMClient() llmClient {
	base := strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	token := ""
	if path := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE"); path != "" {
		if data, err := os.ReadFile(filepath.Clean(path)); err == nil {
			token = strings.TrimSpace(string(data))
		}
	}
	if token == "" {
		token = strings.TrimSpace(os.Getenv("PY_AGENT_INTERNAL_TOKEN"))
	}
	return &httpLLMClient{
		client: &http.Client{Timeout: 360 * time.Second},
		url:    base,
		token:  token,
	}
}

func (c *httpLLMClient) Summarize(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if c.url == "" || c.token == "" {
		return nil, fmt.Errorf("%w: PY_AGENT_INTERPRET_URL/PY_AGENT_INTERNAL_TOKEN 未配置", ErrNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/weekly-summary", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("%w: 周报解析失败（%s）", ErrUpstream, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrUpstream, resp.StatusCode, string(data))
	}
	var out LLMResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: 解码周报响应失败: %w", ErrUpstream, err)
	}
	return &out, nil
}
