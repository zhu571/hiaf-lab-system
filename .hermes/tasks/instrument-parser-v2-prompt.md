实现仪器结果解析器后端。改动说明：

## 背景
Phase 1 计划中的 sweep_s11/find_min_s11/find_max_s11 宏实际未落地到 whitelist_embedded.yaml。
本任务改为：给现有命令加 result_parser，实现解析 + 保存测试数据。

## 改动

### 1. go-server/instruments/model.go
CommandDef 加 ResultParser 字段：
```go
type ResultParserConfig struct {
    Type   string            `yaml:"type" json:"type"`     // "sweep_xy" | "single_value"
    XLabel string            `yaml:"x_label,omitempty" json:"x_label,omitempty"`
    YLabel string            `yaml:"y_label,omitempty" json:"y_label,omitempty"`
    Regex  string            `yaml:"regex,omitempty" json:"regex,omitempty"`
}
```

### 2. whitelist_embedded.yaml
给 E5063A 的 trigger_single 加：
```yaml
  result_parser:
    type: sweep_xy
    x_label: "频率 (Hz)"
    y_label: "S11 (dB)"
    regex: "(?P<points>(?:[\\d.]+,[\\d.-]+(?:;|$))+)" 
```

给 Hioki 的 measure_single 加：
```yaml
  result_parser:
    type: single_value
```

### 3. service.go
新增方法：
```go
type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

type ParsedResult struct {
    Type   string   `json:"type"`
    Points []Point  `json:"points,omitempty"`
    Value  *float64 `json:"value,omitempty"`
    XLabel string   `json:"x_label,omitempty"`
    YLabel string   `json:"y_label,omitempty"`
}

func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult, error) {
    if def.ResultParser == nil {
        return nil, nil
    }
    switch def.ResultParser.Type {
    case "sweep_xy":
        // response contains "freq1,val1;freq2,val2;..." 
        // Split by ; then by ,
        return &ParsedResult{Type: "sweep_xy", Points: points, XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel}, nil
    case "single_value":
        // Extract first float from response
        return &ParsedResult{Type: "single_value", Value: &val}, nil
    }
    return nil, nil
}
```
注意：model.go 已有 Point 类型用于 RF matching，不要重复定义。使用现有类型或在 model.go 加新的 ResultPoint 避免冲突。

### 4. handler.go
新增 POST /{id}/parse-result handler，接收 {command, response}，查 whitelist 找命令定义，调 ParseResult。

### 5. main.go
注册路由，放在已有 instruments 路由组内，与 ListInstruments 同级（requireAuth，不需要 Idempotency-Key）。

验证：go build ./... && go test ./...
