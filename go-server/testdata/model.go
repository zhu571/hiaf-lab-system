package testdata

import (
	"errors"
	"fmt"
	"time"
)

const (
	DataTypeCryo       = "cryo"
	DataTypePressure   = "pressure"
	DataTypeVoltage    = "voltage"
	DataTypeRFVoltage  = "rf_voltage"
	DataTypeEfficiency = "efficiency"

	QualityNormal  = "normal"
	QualityOutlier = "outlier"
	QualitySuspect = "suspect"
	QualityInvalid = "invalid"

	SourceManual     = "manual"
	SourceInstrument = "instrument"
	SourceImport     = "import"
	SourceAgent      = "agent"
	SourceBackfill   = "backfill"
)

type TestData struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	RunID       *string    `json:"run_id,omitempty"`
	DataType    string     `json:"data_type"`
	Measurement string     `json:"measurement"`
	Value       float64    `json:"value"`
	Unit        string     `json:"unit"`
	Quality     string     `json:"quality"`
	Source      string     `json:"source"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	RecordedBy  *string    `json:"recorded_by,omitempty"`
}

type CreateTestDataRequest struct {
	DataType    string     `json:"data_type"`
	RunID       *string    `json:"run_id,omitempty"`
	Measurement string     `json:"measurement"`
	Value       float64    `json:"value"`
	Unit        string     `json:"unit,omitempty"`
	Quality     *string    `json:"quality,omitempty"`
	Source      *string    `json:"source,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
}

type UpdateTestDataRequest struct {
	DataType    *string    `json:"data_type,omitempty"`
	Measurement *string    `json:"measurement,omitempty"`
	Value       *float64   `json:"value,omitempty"`
	Unit        *string    `json:"unit,omitempty"`
	Quality     *string    `json:"quality,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
}

type ListParams struct {
	ProjectID string
	RunID     string
	DataType  string
	Quality   string
	Page      int
	PerPage   int
}

type ListResult struct {
	Items   []TestData `json:"items"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}

// CreateBatchRow 是批量录入的单行请求体，与单条 CreateTestDataRequest 同构。
// Value 使用 *float64：显式区分「缺失」与「0」两个语义（单条端点保留 float64 行为不变）。
type CreateBatchRow struct {
	DataType    string     `json:"data_type"`
	RunID       *string    `json:"run_id,omitempty"`
	Measurement string     `json:"measurement"`
	Value       *float64   `json:"value"`
	Unit        string     `json:"unit,omitempty"`
	Quality     *string    `json:"quality,omitempty"`
	Source      *string    `json:"source,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
}

// RowError 是批量请求中某一行的校验错误；Index 为 0-based 数组下标（即提交时的表格行序）。
// 实现 error 接口以便 repository 直接以 error 返回（FK 竞态兜底路径）。
type RowError struct {
	Index   int    `json:"index"`
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e RowError) Error() string {
	return fmt.Sprintf("第 %d 行 %s 字段：%s", e.Index+1, e.Field, e.Message)
}

// BatchValidationError 携带全部行级错误，handler 命中即返回 422 validation_failed。
type BatchValidationError struct{ Errors []RowError }

func (e *BatchValidationError) Error() string {
	return fmt.Sprintf("%d 行校验失败，请修正后重试", len(e.Errors))
}

// ErrBatchTooLarge：数组长度超限（handler 与 service 双层检查，防绕过）。
var ErrBatchTooLarge = errors.New("batch 超过 100 条上限")

// ErrEmptyBatch：数组为空（handler 层 400 主拦，service 层纵深防线）。
var ErrEmptyBatch = errors.New("batch 不能为空数组")

type BatchCreateResult struct {
	Items []*TestData `json:"items"`
	Count int         `json:"count"`
}
