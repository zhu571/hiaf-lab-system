package todos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// errPublishAuth 表示 ntfy publish 返回 401/403（auth 缓存生效窗口），触发一次重试。
var errPublishAuth = errors.New("ntfy publish auth failed")

// ntfyPublisher 生产发布器：todo-publisher Bearer token，写个人/共享 topic 与 lab-alerts。
type ntfyPublisher struct {
	client *http.Client
	addr   string
	token  string
}

func NewNtfyPublisher() *ntfyPublisher {
	addr := os.Getenv("NTFY_ADDR")
	if addr == "" {
		addr = "http://ntfy:80"
	}
	return &ntfyPublisher{
		client: &http.Client{Timeout: 5 * time.Second},
		addr:   strings.TrimRight(addr, "/"),
		token:  readPublishToken(),
	}
}

// Publish 发布消息；401/403 返回 errPublishAuth（调用方 2s 后重试一次）。
func (p *ntfyPublisher) Publish(topic, title, message string) error {
	req, err := http.NewRequest(http.MethodPost, p.addr+"/"+url.PathEscape(topic), strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("create ntfy publish request: %w", err)
	}
	req.Header.Set("Title", title)
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("publish ntfy: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errPublishAuth
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("ntfy publish returned %s", resp.Status)
	}
}

// readPublishToken 读取 todo-publisher token（文件优先，回退 env）。
func readPublishToken() string {
	if file := os.Getenv("NTFY_PUBLISH_TOKEN_FILE"); file != "" {
		if data, err := os.ReadFile(filepath.Clean(file)); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return strings.TrimSpace(os.Getenv("NTFY_PUBLISH_TOKEN"))
}

// ntfyPublicURL 是 ntfy 对外可达地址（frp 域名或内网 IP），前端拼接订阅地址。
func ntfyPublicURL() string {
	if v := strings.TrimSpace(os.Getenv("NTFY_PUBLIC_URL")); v != "" {
		return v
	}
	return "http://10.144.144.12:8087"
}

// PushDaily 09:00 例行推送：逐用户推送今日在途清单（空清单跳过），按批审计一行。
// 失败计数口径（方案 §9）：任意用户推送失败都计入该批失败——单用户失败不阻塞
// 整批，但本函数返回 error 让 scheduler 计入连续失败告警。
func (s *Service) PushDaily(ctx context.Context) error {
	users, err := s.snap.ActiveUsers()
	if err != nil {
		return err
	}
	pushed := 0
	failed := 0
	for _, u := range users {
		if ctx.Err() != nil {
			break
		}
		if err := s.pushForUser(ctx, u); err != nil {
			// 部分用户失败只对失败者告警（记日志），不阻塞整批。
			logError("push user "+u.Username, err)
			failed++
			continue
		}
		pushed++
	}
	auditErr := s.audit.WriteSystemAudit(ctx, ActionPush, map[string]any{
		"date": s.todayStr(), "users": len(users), "pushed": pushed, "failed": failed,
	})
	if failed > 0 {
		return fmt.Errorf("推送失败 %d/%d 用户", failed, len(users))
	}
	return auditErr
}

func (s *Service) pushForUser(ctx context.Context, u UserSnapshot) error {
	// 推送口径（方案 §3）：个人待办 + 我 active 项目内他人共享的待办。
	projectIDs, err := s.snap.MyProjectIDs(u.ID)
	if err != nil {
		return err
	}
	items, err := s.repo.OpenVisibleForUser(u.ID, projectIDs, s.todayStr())
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil // 空清单跳过
	}
	if err := s.ensureACL(ctx, u); err != nil {
		return err // 新用户 ACL 先写后推，失败计入推送失败
	}
	hints, _ := s.cleanupHintsFor(u.ID)
	title := "📋 今日待办"
	message := renderPushTemplate(s.todayStr(), u, items, hints)
	if err := s.publishWithRetry(topicForUser(u.ID), title, message); err != nil {
		return err
	}
	return nil
}

// publishWithRetry publish 401/403（auth 缓存生效窗口）后 2s 重试一次。
func (s *Service) publishWithRetry(topic, title, message string) error {
	err := s.publisher.Publish(topic, title, message)
	if err != nil && errors.Is(err, errPublishAuth) {
		time.Sleep(s.publishRetryDelay)
		err = s.publisher.Publish(topic, title, message)
	}
	return err
}

// ensureACL 进程内缓存 + 幂等 CLI：首次推送前 EnsureUser（已存在视为成功）+ EnsureAccess。
func (s *Service) ensureACL(ctx context.Context, u UserSnapshot) error {
	s.ensureMu.Lock()
	if s.ensured == nil {
		s.ensured = map[string]bool{}
	}
	if s.ensured[u.ID] {
		s.ensureMu.Unlock()
		return nil
	}
	s.ensureMu.Unlock()

	username := "todo-" + u.Username
	password, err := randomPassword()
	if err != nil {
		return err
	}
	if err := s.ntfy.EnsureUser(username, password); err != nil {
		return err
	}
	if err := s.ntfy.EnsureAccess(topicForUser(u.ID), username, "read-only"); err != nil {
		return err
	}
	s.ensureMu.Lock()
	s.ensured[u.ID] = true
	s.ensureMu.Unlock()
	return nil
}

// renderPushTemplate 组装推送正文：来源标注、优先级排序、清理预警（display_name/标题已清洗）。
func renderPushTemplate(date string, u UserSnapshot, items []Todo, hints []CleanupHint) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 今日待办（%s）\n\n", formatDateCN(date)))
	addedBy := map[string]int{}
	systemCount := 0
	llmCount := 0
	for i, item := range items {
		source := ""
		switch item.Source {
		case SourceIssue:
			source = "（来自 issue）"
			systemCount++
		case SourceDailyLLM:
			source = "（昨日延续）"
			systemCount++
		case SourceLLM:
			// 来源标注四类之一（方案 §3）：LLM 添加
			source = "（LLM 添加）"
			llmCount++
		default:
			name := cleanTitle(item.OwnerDisplayName)
			if name == "" {
				name = item.CreatedBy
			}
			source = "（" + name + "添加）"
			addedBy[name]++
		}
		title := cleanTitle(item.Title)
		b.WriteString(fmt.Sprintf("%d. ☐ [%s] %s%s\n", i+1, priorityLabel(item.Priority), title, source))
	}
	var footer []string
	for name, n := range addedBy {
		footer = append(footer, fmt.Sprintf("%d 条由%s添加", n, name))
	}
	if llmCount > 0 {
		footer = append(footer, fmt.Sprintf("%d 条由 LLM 添加", llmCount))
	}
	if systemCount > 0 {
		footer = append(footer, fmt.Sprintf("%d 条由系统生成", systemCount))
	}
	if len(footer) > 0 {
		b.WriteString("—— " + strings.Join(footer, "，") + "\n")
	}
	if len(hints) > 0 {
		groups := make([]string, 0, len(hints))
		for _, h := range hints {
			groups = append(groups, fmt.Sprintf("%d 条 %d 天后", h.Count, h.DaysLeft))
		}
		b.WriteString("\n⚠️ 清理提醒：" + strings.Join(groups, "、") + "将被自动清理，请及时处理")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDateCN(date string) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf("%d月%d日 %s", int(t.Month()), t.Day(), weekdays[t.Weekday()])
}

func priorityLabel(p string) string {
	switch p {
	case PriorityHigh:
		return "高"
	case PriorityLow:
		return "低"
	default:
		return "中"
	}
}
