package todos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// httpLLMPlanner 生产 LLM 客户端：调 py-agent /v1/todo-add（15s）与 /v1/todo-daily（60s），
// 内部 token 鉴权（对齐 steptemplates AutoConfigure：PY_AGENT_INTERPRET_URL + PY_AGENT_INTERNAL_TOKEN_FILE）。
type httpLLMPlanner struct {
	client *http.Client
	url    string
	token  string
}

func NewHTTPLLMPlanner() llmPlanner {
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
	return &httpLLMPlanner{
		client: &http.Client{Timeout: 60 * time.Second},
		url:    base,
		token:  token,
	}
}

func (p *httpLLMPlanner) post(ctx context.Context, path string, timeout time.Duration, payload any, out any) error {
	if p.url == "" || p.token == "" {
		return fmt.Errorf("py-agent 服务未配置")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("py-agent 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("py-agent 解析失败（%s）", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("py-agent 返回 %d: %s", resp.StatusCode, string(data))
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

// ParseAdd 调 /v1/todo-add（15s 超时，照 instruments interpret 先例）。
func (p *httpLLMPlanner) ParseAdd(ctx context.Context, userID, rawText string) (*LLMParseResponse, error) {
	var resp LLMParseResponse
	err := p.post(ctx, "/v1/todo-add", 15*time.Second, map[string]any{
		"raw_text": rawText, "user_id": userID,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GenerateDaily 调 /v1/todo-daily（60s 超时，照 step-plan 先例）。
func (p *httpLLMPlanner) GenerateDaily(ctx context.Context, userID, report string, issues []IssueSnapshot, existingTitles []string) ([]LLMItem, error) {
	openIssues := make([]map[string]any, 0, len(issues))
	for _, iss := range issues {
		openIssues = append(openIssues, map[string]any{
			"id": iss.ID, "title": iss.Title, "severity": iss.Severity,
		})
	}
	var resp struct {
		Status string    `json:"status"`
		Items  []LLMItem `json:"items"`
	}
	err := p.post(ctx, "/v1/todo-daily", 60*time.Second, map[string]any{
		"user_id": userID, "yesterday_report": report,
		"open_issues": openIssues, "existing_titles": existingTitles,
	}, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("todo-daily 返回无效状态: %s", resp.Status)
	}
	if resp.Items == nil {
		resp.Items = []LLMItem{}
	}
	return resp.Items, nil
}

// httpReportFetcher 生产日报获取器：经 by-date service token 端点拉取用户最近一份非空日报。
type httpReportFetcher struct {
	client *http.Client
	base   string
}

func NewHTTPReportFetcher(selfBase string) reportFetcher {
	return &httpReportFetcher{
		client: &http.Client{Timeout: 15 * time.Second},
		base:   strings.TrimRight(selfBase, "/"),
	}
}

func (f *httpReportFetcher) FetchLatestReport(ctx context.Context, userID string) (string, error) {
	u := f.base + "/api/v1/daily-reports/by-date?latest=true&user_id=" + url.QueryEscape(userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+middleware.ReadServiceToken())
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("拉取日报失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // 零日报用户视同无日报
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("日报端点返回 %d: %s", resp.StatusCode, string(data))
	}
	var envelope struct {
		Data struct {
			RawText string `json:"raw_text"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 512<<10))
	if err := decoder.Decode(&envelope); err != nil {
		return "", fmt.Errorf("解码日报响应失败: %w", err)
	}
	return envelope.Data.RawText, nil
}
