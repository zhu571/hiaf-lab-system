实现功能二：仪器测量保存测试数据 + 曲线可视化。

## 设计决策
1. 手动命令 + AI 对话 两处都加「保存到测试数据」按钮
2. 扫频命令（sweep_s11）执行后自动渲染 Chart.js 曲线图
3. 特定命令写解析器（result_parser），通用命令用正则提取数值
4. 保存弹窗：选项目 + 选批次（可选）+ 数据类型 + 测量项 + 数值 + 单位

## 改动

### 后端

#### 1. go-server/instruments/model.go
CommandDef 增加字段：
```go
type CommandDef struct {
    ...
    ResultParser *ResultParserConfig `yaml:"result_parser" json:"result_parser,omitempty"`
}

type ResultParserConfig struct {
    Type   string            `yaml:"type" json:"type"`     // "sweep_xy" | "single_value" | "min_freq"
    XLabel string            `yaml:"x_label" json:"x_label,omitempty"`
    YLabel string            `yaml:"y_label" json:"y_label,omitempty"`
    Regex  string            `yaml:"regex,omitempty" json:"regex,omitempty"`
    Fields map[string]string `yaml:"fields,omitempty" json:"fields,omitempty"` // 命名捕获组名 → 字段名
}
```

#### 2. go-server/instruments/whitelist_embedded.yaml
给扫频命令加 result_parser:
```yaml
- name: sweep_s11
  ...
  result_parser:
    type: sweep_xy
    x_label: 频率 (MHz)
    y_label: S11 (dB)
    regex: (?P<points>(?:[\d.]+,[\d.]+(?:;|$))+)
```
find_min_s11 / find_max_s11 同。

Hioki IM3536 的 measure 命令：
```yaml
- name: trigger_measure
  ...
  result_parser:
    type: single_value
    fields:
      value: (?<=,)([\d.]+)
```

#### 3. go-server/instruments/service.go
新增 ParseResult 方法：
```go
type ParsedResult struct {
    Type   string         `json:"type"`
    Points []Point        `json:"points,omitempty"`
    Value  *float64       `json:"value,omitempty"`
    Fields map[string]any `json:"fields,omitempty"`
}

type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

func (s *Service) ParseResult(parser *ResultParserConfig, response string) (*ParsedResult, error) {
    switch parser.Type {
    case "sweep_xy":
        // Parse "freq,value;freq,value;..." format
        // Return sweep points
    case "single_value":
        // Extract first float from response
    }
}
```

#### 4. go-server/instruments/handler.go
新增端点：
```go
r.Post("/{id}/parse-result", instrumentsHandler.ParseResult)
```

### 前端

#### 5. api/instruments.ts
新增类型和函数：
```typescript
export interface ParsedResult {
  type: 'sweep_xy' | 'single_value'
  points?: { x: number; y: number }[]
  value?: number
  fields?: Record<string, any>
}

export function parseResult(instrumentId: string, response: string) {
  return request<ParsedResult>({ url: `/instruments/${instrumentId}/parse-result`, method: 'POST', data: { response } })
}
```

#### 6. InstrumentMeasureView.vue
- cmdResult 区块：
  - 如果 cmdResult 有 parser 且 parser.type='sweep_xy'，渲染 Chart.js 折线图（频率 vs S11）
  - 曲线图上标注 min/max 点（如果命令是 find_min/max）
  - 增加「保存到测试数据」按钮

- 保存弹窗：
  - 项目下拉（listProjects）
  - 批次下拉（选择项目后 listRuns）
  - 数据类型下拉（cryo/pressure/voltage/rf_voltage/efficiency）
  - 测量项（预填命令名）
  - 数值（预填解析结果）
  - 单位
  - 测量时间（默认当前时间）
  - 备注（自动填入 instrument 和 command 信息）

#### 7. AI 对话中的保存
在 InstrumentMeasureView 的 AI 对话区域，每步执行结果显示后加「保存」按钮，同样弹出保存弹窗。

验证：go build + go test + npm run build
