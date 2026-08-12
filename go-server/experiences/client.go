package experiences

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

// httpExtractClient 生产 LLM 客户端：调 py-agent /v1/experience-extract（300s 超时——
// 单步 LLM 预算 240s + 余量；对齐 weekly httpLLMClient 的 AutoConfigure 鉴权模式）。
type httpExtractClient struct {
	client *http.Client
	url    string
	token  string
}

func NewHTTPExtractClient() extractLLM {
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
	return &httpExtractClient{
		client: &http.Client{Timeout: 300 * time.Second},
		url:    base,
		token:  token,
	}
}

func (c *httpExtractClient) Extract(ctx context.Context, req ExtractLLMRequest) (*ExtractLLMResponse, error) {
	if c.url == "" || c.token == "" {
		return nil, fmt.Errorf("%w: PY_AGENT_INTERPRET_URL/PY_AGENT_INTERNAL_TOKEN 未配置", ErrExtractNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/experience-extract", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrExtractUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("%w: 经验提取解析失败（%s）", ErrExtractUpstream, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrExtractUpstream, resp.StatusCode, string(data))
	}
	var out ExtractLLMResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: 解码经验提取响应失败: %w", ErrExtractUpstream, err)
	}
	return &out, nil
}
