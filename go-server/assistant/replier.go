package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Replier 是 assistant 的 LLM 窄接口；真实实现调 py-agent，测试可注入 fake。
type Replier interface {
	Reply(ctx context.Context, messages []Message) (string, error)
}

type httpReplier struct {
	client *http.Client
	url    string
	token  string
}

// NewHTTPReplier 读 PY_AGENT_INTERPRET_URL / PY_AGENT_INTERNAL_TOKEN_FILE；
// URL 未配置时返回 nil，Service 回退到占位回复。
func NewHTTPReplier() Replier {
	base := strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	if base == "" {
		return nil
	}
	token := ""
	if path := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE"); path != "" {
		if data, err := os.ReadFile(filepath.Clean(path)); err == nil {
			token = strings.TrimSpace(string(data))
		}
	}
	return &httpReplier{
		client: &http.Client{Timeout: 120 * time.Second},
		url:    base + "/v1/assistant-reply",
		token:  token,
	}
}

func (r *httpReplier) Reply(ctx context.Context, messages []Message) (string, error) {
	payload := struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{}
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		payload.Messages = append(payload.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("assistant reply request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("assistant reply status %d", resp.StatusCode)
	}
	var out struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode assistant reply: %w", err)
	}
	if strings.TrimSpace(out.Answer) == "" {
		return "", fmt.Errorf("empty assistant reply")
	}
	return out.Answer, nil
}
