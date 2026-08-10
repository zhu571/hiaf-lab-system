// Package notify sends lab alerts through ntfy.
package notify

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAddr = "http://ntfy:80"
	// AlertTopic 是 ntfy 告警主题（todos scheduler 失败告警复用）。
	AlertTopic = "lab-alerts"
	WebURL     = "http://10.144.144.12:8000"
)

var client = &http.Client{Timeout: 5 * time.Second}

// Send pushes a notification to ntfy.
func readPublishToken() string {
	if file := os.Getenv("NTFY_PUBLISH_TOKEN_FILE"); file != "" {
		if data, err := os.ReadFile(file); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return strings.TrimSpace(os.Getenv("NTFY_PUBLISH_TOKEN"))
}

func Send(topic, title, message, clickURL, priority string, tags []string) error {
	addr := os.Getenv("NTFY_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(addr, "/")+"/"+url.PathEscape(topic), strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("create ntfy request: %w", err)
	}
	req.Header.Set("Title", title)
	req.Header.Set("Click", clickURL)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", strings.Join(tags, ","))
	// v13：统一切换为 todo-publisher Bearer token（NTFY_USER/NTFY_PASS 已废弃）。
	if token := readPublishToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send ntfy request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ntfy returned %s", resp.Status)
	}
	return nil
}

func meowSend(title, body string) error {
	req, err := http.NewRequest(http.MethodPost, "https://api.chuckfang.com/f064e4e8/"+url.PathEscape(title)+"?msgType=markdown", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create MeoW request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send MeoW request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("MeoW returned %s", resp.Status)
	}
	return nil
}

// SendBoth 走 ntfy + MeoW 双通道（等价旧 sendBoth，供 alert 模块 critical/error
// 上报适配器复用；返回值语义与 Send 一致——ntfy 失败即返回错误，MeoW 失败仅日志）。
func SendBoth(topic, title, message, clickURL, priority string, tags []string) error {
	ntfyErr := Send(topic, title, message, clickURL, priority, tags)
	if err := meowSend(title, message); err != nil {
		log.Printf("MeoW notification failed: %v", err)
	}
	return ntfyErr
}

// InstrumentEmergency reports an instrument emergency stop.
func InstrumentEmergency(instrument, user string) error {
	return SendBoth(AlertTopic, "仪器急停", fmt.Sprintf("%s 被 %s 紧急停止", instrument, user), WebURL+"/", "urgent", []string{"rotating_light"})
}

// InstrumentRestoreFailed reports that an instrument could not be restored.
func InstrumentRestoreFailed(instrument, err string) error {
	return SendBoth(AlertTopic, "仪器恢复失败", fmt.Sprintf("%s: %s", instrument, err), WebURL+"/", "high", []string{"warning"})
}

// SecurityAlert reports a security event.
func SecurityAlert(title, detail string) error {
	return SendBoth(AlertTopic, title, detail, WebURL+"/audit", "urgent", []string{"shield"})
}
