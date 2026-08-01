package system

import (
	"encoding/json"
	"fmt"
	"strings"
)

// composePSContainer 对应 `docker compose ps --format json` 的单个容器对象。
type composePSContainer struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// parseComposePS 解析 `compose ps --format json` 输出。
// compose v2 单服务过滤时返回单个对象，无过滤时返回数组，两种形态都要兼容。
func parseComposePS(data []byte) ([]composePSContainer, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var out []composePSContainer
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("解析 compose ps 数组失败: %w", err)
		}
		return out, nil
	}
	var one composePSContainer
	if err := json.Unmarshal(data, &one); err != nil {
		return nil, fmt.Errorf("解析 compose ps 单对象失败: %w", err)
	}
	return []composePSContainer{one}, nil
}

// containerHealth 提取容器健康态，与 update.sh 的 python 逻辑等价：
// Health 非空用 Health，否则退回 State（无健康检查的服务以 running 视为健康）。
func containerHealth(c composePSContainer) string {
	if c.Health != "" {
		return c.Health
	}
	return c.State
}

// parseComposeServices 解析 `compose config --services` 输出（JSON 字符串数组）。
func parseComposeServices(data []byte) ([]string, error) {
	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 compose config --services 失败: %w", err)
	}
	return out, nil
}

// parseComposeImages 解析 `compose config --images` 输出（JSON 字符串数组）。
func parseComposeImages(data []byte) ([]string, error) {
	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 compose config --images 失败: %w", err)
	}
	return out, nil
}

// composeProject 对应 `docker compose ls --format json` 的单个项目对象。
type composeProject struct {
	Name       string `json:"Name"`
	Status     string `json:"Status"`
	ConfigFile string `json:"ConfigFiles"`
}

// parseComposeProjects 解析 `docker compose ls --format json` 输出（数组）。
func parseComposeProjects(data []byte) ([]composeProject, error) {
	var out []composeProject
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 compose ls 失败: %w", err)
	}
	return out, nil
}
