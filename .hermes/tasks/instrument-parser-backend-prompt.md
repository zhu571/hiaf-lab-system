实现仪器结果解析器后端。在现有的 instruments 模块中增加 result_parser 支持。

## 改动

### 1. go-server/instruments/model.go
CommandDef 增加 ResultParser 字段：
```go
type ResultParserConfig struct {
    Type   string            `yaml:"type" json:"type"`     // "sweep_xy" | "single_value"
    XLabel string            `yaml:"x_label,omitempty" json:"x_label,omitempty"`
    YLabel string            `yaml:"y_label,omitempty" json:"y_label,omitempty"`
    Fields map[string]string `yaml:"fields,omitempty" json:"fields,omitempty"`
}
```
并在 CommandDef 中加 `ResultParser *ResultParserConfig ...`（yaml/json 标签，omitempty）

### 2. go-server/instruments/whitelist_embedded.yaml
给 E5063A 的扫频命令加 result_parser：
- sweep_s11: type=sweep_xy
- find_min_s11: type=sweep_xy
- find_max_s11: type=sweep_xy
给 Hioki 的 trigger_measure 和 read_impedance 加 type=single_value

### 3. go-server/instruments/service.go
新增 ParseResult 方法和返回类型：
```go
type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

type ParsedResult struct {
    Type   string         `json:"type"`
    Points []Point        `json:"points,omitempty"`
    Value  *float64       `json:"value,omitempty"`
}

func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult, error)
```
sweep_xy 解析：response 格式为 "freq1,val1;freq2,val2;..."，用正则拆分后返回 Point 列表。
single_value：提取响应中第一个浮点数。

### 4. go-server/instruments/handler.go
新增 ParseResult handler + 路由注册（POST /{id}/parse-result）

验证：go build ./... && go test ./...
