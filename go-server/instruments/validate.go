package instruments

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

// NormalizeParams applies defaults and every deterministic whitelist constraint.
func NormalizeParams(instrumentID, commandName string, input map[string]any) (map[string]any, error) {
	def, err := GetCommand(instrumentID, commandName)
	if err != nil {
		return nil, err
	}
	params := make(map[string]any, len(def.Params))
	for name := range input {
		if _, ok := def.Params[name]; !ok {
			return nil, fmt.Errorf("参数 %s 不在白名单中", name)
		}
	}
	for name, rawSchema := range def.Params {
		schema, ok := rawSchema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("参数 %s 定义无效", name)
		}
		value, exists := input[name]
		if !exists {
			value, exists = schema["default"]
		}
		if !exists {
			return nil, fmt.Errorf("缺少参数 %s", name)
		}
		normalized, err := normalizeValue(name, value, schema)
		if err != nil {
			return nil, err
		}
		params[name] = normalized
	}
	if err := validateObjectConstraints(def, params); err != nil {
		return nil, err
	}
	if commandName == "set_sweep_range" {
		start, stop := params["start_freq"].(float64), params["stop_freq"].(float64)
		if start >= stop {
			return nil, fmt.Errorf("扫频起点必须小于终点")
		}
		if stop-start > 100e6 {
			return nil, fmt.Errorf("单次扫频跨度不得超过 100 MHz")
		}
		points, ifbw := params["points"].(int), params["if_bandwidth"].(float64)
		if float64(points)/ifbw*1000 > float64(def.TimeoutMS) {
			return nil, fmt.Errorf("预计扫频时间超过命令超时")
		}
	}
	return params, nil
}

func normalizeValue(name string, value any, schema map[string]any) (any, error) {
	if choices, ok := schema["enum"].([]any); ok {
		for _, choice := range choices {
			if fmt.Sprint(value) == fmt.Sprint(choice) {
				return choice, nil
			}
		}
		return nil, fmt.Errorf("参数 %s 不在允许值中", name)
	}
	switch schema["type"] {
	case "float":
		value, ok := number(value)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("参数 %s 必须是有限数值", name)
		}
		if err := validateRange(name, value, schema); err != nil {
			return nil, err
		}
		return value, nil
	case "int":
		value, ok := number(value)
		if !ok || math.Trunc(value) != value {
			return nil, fmt.Errorf("参数 %s 必须是整数", name)
		}
		if err := validateRange(name, value, schema); err != nil {
			return nil, err
		}
		return int(value), nil
	case "string":
		value, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("参数 %s 必须是字符串", name)
		}
		if max, ok := number(schema["max_len"]); ok && len(value) > int(max) {
			return nil, fmt.Errorf("参数 %s 过长", name)
		}
		for _, denied := range stringsList(schema["deny_patterns"]) {
			if strings.Contains(value, denied) {
				return nil, fmt.Errorf("参数 %s 含禁止内容", name)
			}
		}
		if pattern, _ := schema["regex"].(string); pattern != "" {
			matched, err := regexp.MatchString(pattern, value)
			if err != nil || !matched {
				return nil, fmt.Errorf("参数 %s 格式无效", name)
			}
		}
		if extensions := stringsList(schema["allow_extensions"]); len(extensions) > 0 {
			ext := strings.ToLower(filepath.Ext(value))
			if !contains(extensions, ext) {
				return nil, fmt.Errorf("参数 %s 扩展名不允许", name)
			}
		}
		return value, nil
	default:
		return nil, fmt.Errorf("参数 %s 类型定义无效", name)
	}
}

func validateObjectConstraints(def *CommandDef, params map[string]any) error {
	if len(def.ObjectConstraints) == 0 {
		return nil
	}
	objectType, _ := params["object_type"].(string)
	if objectType == "" {
		objectType = "unknown"
	}
	constraints, ok := def.ObjectConstraints[objectType]
	if !ok {
		return fmt.Errorf("对象类型 %s 无约束定义", objectType)
	}
	if dc, ok := constraints["dc_bias"].(map[string]any); ok && dc["enabled"] == false {
		return fmt.Errorf("%v", dc["reject_reason"])
	}
	for name, raw := range constraints {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		value, exists := params[name]
		if !exists {
			continue
		}
		n, ok := number(value)
		if ok {
			if err := validateRange(name, n, rule); err != nil {
				return err
			}
		}
	}
	if maxSpan, ok := number(constraints["max_span_hz"]); ok {
		if params["stop_freq"].(float64)-params["start_freq"].(float64) > maxSpan {
			return fmt.Errorf("扫频跨度超过对象类型 %s 的安全上限", objectType)
		}
	}
	return nil
}

func validateRange(name string, value float64, schema map[string]any) error {
	if min, ok := number(schema["min"]); ok && value < min {
		return fmt.Errorf("参数 %s 小于安全下限 %v", name, min)
	}
	if max, ok := number(schema["max"]); ok && value > max {
		return fmt.Errorf("参数 %s 超过安全上限 %v", name, max)
	}
	return nil
}

func number(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func stringsList(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.ToLower(fmt.Sprint(item)))
	}
	return out
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// RenderSCPI only renders values after deterministic whitelist validation.
func RenderSCPI(instrumentID, commandName string, input map[string]any) (string, map[string]any, error) {
	params, err := NormalizeParams(instrumentID, commandName, input)
	if err != nil {
		return "", nil, err
	}
	def, _ := GetCommand(instrumentID, commandName)
	template := def.Build
	if template == "" {
		template = def.SCPI
	}
	for name, value := range params {
		template = strings.ReplaceAll(template, "{"+name+"}", fmt.Sprint(value))
	}
	if strings.Contains(template, "{") {
		return "", nil, fmt.Errorf("命令模板仍有未解析参数")
	}
	return strings.TrimSpace(template), params, nil
}
