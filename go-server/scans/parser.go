package scans

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// 扫描文件格式（scan-archive v0.4 §7.1）。
const (
	FormatLegacy3 = "legacy3"
	FormatV7      = "v7"
	FormatV11     = "v11"
	FormatV12     = "v12"
	FormatUnknown = "unknown"

	// MaxParseRows 是单次返回/存储的行上限；超出截断并标记 Truncated。
	MaxParseRows = 50000
)

// ParseResult 是上传时的轻量解析结果；不存逐点数据，详情页按需重解析。
type ParseResult struct {
	Format    string
	Columns   []string
	Rows      [][]*float64
	RowCount  int
	BadRows   int
	Warnings  []string
	Truncated bool
}

// ParseTXT 解析 tab 分隔的扫描 txt（设计 §7）。
// 无法识别/非 UTF-8/空文件不报错：返回 FormatUnknown + 警告，仍允许存档。
func ParseTXT(data []byte) ParseResult {
	res := ParseResult{Format: FormatUnknown, Columns: []string{}, Rows: [][]*float64{}}
	data = stripBOM(data)
	if len(data) == 0 {
		res.Warnings = append(res.Warnings, "empty_file")
		return res
	}
	if !utf8.Valid(data) {
		res.Warnings = append(res.Warnings, "invalid_utf8")
		return res
	}

	lines := splitLines(string(data))
	headerIdx := -1
	var header []string
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := splitFields(line)
		if isHeader(fields) {
			headerIdx = i
			header = fields
			break
		}
		break
	}

	if headerIdx >= 0 {
		switch len(header) {
		case 7:
			res.Format = FormatV7
		case 11:
			res.Format = FormatV11
		case 12:
			res.Format = FormatV12
		default:
			res.Warnings = append(res.Warnings, "unrecognized_header")
			return res
		}
		res.Columns = header
		res.Rows, res.RowCount, res.BadRows = parseRows(lines[headerIdx+1:])
		res.Truncated = len(res.Rows) >= MaxParseRows
		return res
	}

	// 无表头：只有 3 列数值视为 legacy3（只存档，不作图）。
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := splitFields(line)
		if len(fields) != 3 || !allNumeric(fields) {
			res.Warnings = append(res.Warnings, "unrecognized_format")
			return res
		}
		break
	}
	res.Format = FormatLegacy3
	res.Warnings = append(res.Warnings, "legacy_format_no_chart")
	_, res.RowCount, res.BadRows = parseRows(lines)
	return res
}

func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func splitFields(line string) []string {
	raw := strings.Split(line, "\t")
	out := make([]string, len(raw))
	for i, field := range raw {
		out[i] = strings.TrimSpace(field)
	}
	return out
}

func isHeader(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if field == "" {
			return false
		}
		if _, err := strconv.ParseFloat(field, 64); err != nil {
			return true
		}
	}
	return false
}

func allNumeric(fields []string) bool {
	for _, field := range fields {
		if _, err := strconv.ParseFloat(field, 64); err != nil {
			return false
		}
	}
	return true
}

func parseRows(lines []string) ([][]*float64, int, int) {
	rows := make([][]*float64, 0, len(lines))
	bad := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(rows) >= MaxParseRows {
			break
		}
		fields := splitFields(line)
		row := make([]*float64, len(fields))
		valid := 0
		for i, field := range fields {
			if field == "" {
				continue
			}
			value, err := strconv.ParseFloat(field, 64)
			if err != nil {
				bad++
				row = nil
				break
			}
			row[i] = &value
			valid++
		}
		if row == nil || valid == 0 {
			if row != nil {
				bad++
			}
			continue
		}
		rows = append(rows, row)
	}
	return rows, len(rows), bad
}
