package system

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Logger 把流水线的文本行以"行流"写入日志文件（无缓冲，天然行级可见），
// 并可同时 tee 到 stdout（runner 容器内 docker logs 可见）。
// Go 侧 tailSessionLog 以 200ms 轮询该文件，因此每行必须以换行结尾即时可见。
type Logger struct {
	mu   sync.Mutex
	file *os.File
	tee  io.Writer
}

// NewLogger 打开/创建日志文件并可选 tee 到标准输出。
// filePath 为空时只写 tee（用于本地调试）。
func NewLogger(filePath string, tee io.Writer) (*Logger, error) {
	l := &Logger{tee: tee}
	if filePath != "" {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("打开日志文件失败: %w", err)
		}
		l.file = f
	}
	return l, nil
}

// Linef 写一行日志（自动补换行），并发安全。
func (l *Logger) Linef(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_, _ = l.file.WriteString(line)
	}
	if l.tee != nil {
		_, _ = io.WriteString(l.tee, line)
	}
}

// Close 关闭日志文件。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// DoneMarker 是 done marker 的磁盘契约（I3），字段与 update.sh EXIT trap 完全一致。
type DoneMarker struct {
	ExitCode int    `json:"exit_code"`
	OldSHA   string `json:"old_sha"`
	NewSHA   string `json:"new_sha"`
	EndedAt  string `json:"ended_at"`
}

// WriteDoneMarker 幂等写入 done marker 文件，格式与 finishFromMarker/recoverFromDisk 兼容。
func WriteDoneMarker(path string, m DoneMarker) error {
	if path == "" {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("序列化 marker 失败: %w", err)
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("写 marker 失败: %w", err)
	}
	return nil
}

// writeFileAtomic 先写临时文件再 rename 原子替换，避免 tail/恢复方读到写一半的文件。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// nowUTC 返回 marker 使用的结束时间（与 update.sh date -u 一致，RFC3339 Z）。
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
