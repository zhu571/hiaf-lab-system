package testdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type CurveHandler struct{ *Handler }

func NewCurveHandler(svc *Service) *CurveHandler { return &CurveHandler{Handler: NewHandler(svc)} }

const curveMaxBodyBytes = 512 << 10

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data.create")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateTestDataRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	td, err := h.svc.Create(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, r.Header, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, td)
}

// CreateBatch 批量录入：请求体为数组（≤100 条），事务原子，任一失败整批回滚并逐行报错。
func (h *Handler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "testdata.batch")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	// 先解出 []json.RawMessage：非数组/空数组 → 400（结构性问题，非行级）；
	// 长度 > 100 → 422 batch_too_large（不解析元素）。
	var raws []json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, batchMaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&raws); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			common.WriteError(w, r, http.StatusRequestEntityTooLarge, "request_too_large",
				"请求体超过 512KB 上限", map[string]any{"max": batchMaxBodyBytes})
			return
		}
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体必须是 JSON 数组", nil)
		return
	}
	if len(raws) == 0 {
		middleware.SetAuditDetail(r.Context(), map[string]any{"count": 0, "received": 0})
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", ErrEmptyBatch.Error(), nil)
		return
	}
	if len(raws) > batchMaxRows {
		middleware.SetAuditDetail(r.Context(), map[string]any{"count": len(raws), "received": len(raws)})
		common.WriteError(w, r, http.StatusUnprocessableEntity, "batch_too_large", ErrBatchTooLarge.Error(),
			map[string]any{"max": batchMaxRows, "received": len(raws)})
		return
	}
	// 逐元素 DisallowUnknownFields 解码；解码失败收集该行错误继续（不遇错即停）。
	// 占位空行保持 rows 下标与请求数组对齐（行级错误按 index 指向请求下标）；
	// decodeFailed 记录解码失败行下标，service 据此跳过语义校验（避免占位零值补出假 required）。
	rows := make([]CreateBatchRow, len(raws))
	decodeFailed := make(map[int]bool)
	var decodeErrors []RowError
	for i, raw := range raws {
		row, rowErrors := decodeBatchRow(i, raw)
		if len(rowErrors) > 0 {
			decodeErrors = append(decodeErrors, rowErrors...)
			decodeFailed[i] = true
			continue
		}
		rows[i] = row
	}
	result, err := h.svc.CreateBatch(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, r.Header, rows, decodeFailed, decodeErrors)
	if err != nil {
		// 422 失败也由 Audit 中间件落一条 testdata.batch 记录（含 error_rows），便于追查被拒绝的批量。
		var batchErr *BatchValidationError
		if errors.As(err, &batchErr) {
			middleware.SetAuditDetail(r.Context(), map[string]any{"count": len(rows), "error_rows": len(batchErr.Errors)})
			common.WriteError(w, r, http.StatusUnprocessableEntity, "validation_failed", batchErr.Error(),
				map[string]any{"errors": batchErr.Errors})
			return
		}
		if errors.Is(err, ErrBatchTooLarge) {
			common.WriteError(w, r, http.StatusUnprocessableEntity, "batch_too_large", err.Error(),
				map[string]any{"max": batchMaxRows, "received": len(rows)})
			return
		}
		if errors.Is(err, ErrEmptyBatch) {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
			return
		}
		h.writeError(w, r, err)
		return
	}
	// 审计整批一条：详情含条数与创建 id 列表（顺序 = 请求行序），不刷 N 条。
	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{"count": result.Count, "created_ids": ids})
	common.WriteCreated(w, r, result)
}

// batchMaxBodyBytes：512KB 防御纵深，100 行上限下足够。
const batchMaxBodyBytes = 512 << 10

// batchAllowedFields 与 CreateBatchRow 字段白名单一致。
var batchAllowedFields = map[string]bool{
	"data_type": true, "run_id": true, "measurement": true, "value": true,
	"unit": true, "quality": true, "source": true, "measured_at": true, "notes": true,
}

// decodeBatchRow 行级 JSON 结构解析（handler 职责边界，service 只做语义层）：
// 非对象 → invalid_row（field=body）；未知字段 → unknown_field（field=未知键名）；
// 值类型错误 → 逐个字段探针定位失败字段（invalid_row）。
func decodeBatchRow(index int, raw json.RawMessage) (CreateBatchRow, []RowError) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return CreateBatchRow{}, []RowError{{Index: index, Field: "body", Code: "invalid_row", Message: "该行必须是 JSON 对象"}}
	}
	for key := range probe {
		if !batchAllowedFields[key] {
			return CreateBatchRow{}, []RowError{{Index: index, Field: key, Code: "unknown_field", Message: "未知字段 " + key}}
		}
	}
	var row CreateBatchRow
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&row); err != nil {
		fields := batchRowInvalidFields(probe)
		rowErrors := make([]RowError, 0, len(fields))
		for _, field := range fields {
			rowErrors = append(rowErrors, RowError{Index: index, Field: field, Code: "invalid_row", Message: "字段格式不正确"})
		}
		return CreateBatchRow{}, rowErrors
	}
	return row, nil
}

var batchFieldTypeProbes = []struct {
	name  string
	probe func(raw json.RawMessage) error
}{
	{"data_type", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"measurement", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"value", func(raw json.RawMessage) error { var v float64; return json.Unmarshal(raw, &v) }},
	{"unit", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"quality", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"source", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"measured_at", func(raw json.RawMessage) error { var v time.Time; return json.Unmarshal(raw, &v) }},
	{"run_id", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
	{"notes", func(raw json.RawMessage) error { var v string; return json.Unmarshal(raw, &v) }},
}

// batchRowInvalidFields 按字段序找出解码失败的具体字段（值类型错误时精确定位到列）。
func batchRowInvalidFields(probe map[string]json.RawMessage) []string {
	var fields []string
	for _, f := range batchFieldTypeProbes {
		raw, ok := probe[f.name]
		if !ok {
			continue
		}
		if err := f.probe(raw); err != nil {
			fields = append(fields, f.name)
		}
	}
	if len(fields) == 0 {
		return []string{"body"}
	}
	return fields
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.List(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, ListParams{
		RunID: r.URL.Query().Get("run_id"), DataType: r.URL.Query().Get("data_type"),
		Quality: r.URL.Query().Get("quality"), Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	td, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, td)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data.update")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	req, err := decodeUpdateRequest(r)
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体包含不可修改字段或无法解析", nil)
		return
	}
	td, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, td)
}

func (h *Handler) MarkInvalid(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data.delete")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.MarkInvalid(id, middleware.EffectiveUserID(r.Context()), claims.Role); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func (h *CurveHandler) Create(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data_curve.create")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, curveMaxBodyBytes)
	var req CreateCurveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeCurveDecodeError(w, r, err)
		return
	}
	curve, err := h.svc.CreateCurve(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, r.Header, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, curve)
}

func (h *CurveHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.ListCurves(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, ListCurvesParams{
		RunID: r.URL.Query().Get("run_id"), CurveType: r.URL.Query().Get("curve_type"),
		Quality: r.URL.Query().Get("quality"), Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *CurveHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	curve, err := h.svc.GetCurve(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, curve)
}

func (h *CurveHandler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data_curve.update")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, curveMaxBodyBytes)
	req, err := decodeCurveUpdateRequest(r)
	if err != nil {
		writeCurveDecodeError(w, r, err)
		return
	}
	curve, err := h.svc.UpdateCurve(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, curve)
}

func (h *CurveHandler) Void(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "test_data_curve.delete")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
			return
		}
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.VoidCurve(id, middleware.EffectiveUserID(r.Context()), claims.Role, req.Reason); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func decodeCurveUpdateRequest(r *http.Request) (UpdateCurveRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return UpdateCurveRequest{}, err
	}
	allowed := map[string]bool{
		"name": true, "x_label": true, "y_label": true, "unit": true, "points": true,
		"quality": true, "notes": true, "measured_at": true,
	}
	for field := range raw {
		if !allowed[field] {
			return UpdateCurveRequest{}, errors.New("immutable or unknown field")
		}
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return UpdateCurveRequest{}, err
	}
	var req UpdateCurveRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	return req, err
}

func writeCurveDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		common.WriteError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过 512KB 上限", nil)
		return
	}
	common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体包含不允许的字段或无法解析", nil)
}

func decodeUpdateRequest(r *http.Request) (UpdateTestDataRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return UpdateTestDataRequest{}, err
	}
	allowed := map[string]bool{
		"measurement": true, "value": true, "unit": true, "quality": true, "measured_at": true, "notes": true,
	}
	for field := range raw {
		if !allowed[field] {
			return UpdateTestDataRequest{}, errors.New("immutable or unknown field")
		}
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return UpdateTestDataRequest{}, err
	}
	var req UpdateTestDataRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	return req, err
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrCurveNotFound):
		common.WriteError(w, r, http.StatusNotFound, "test_data_curve_not_found", err.Error(), nil)
	case errors.Is(err, ErrTestDataNotFound):
		common.WriteError(w, r, http.StatusNotFound, "test_data_not_found", err.Error(), nil)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), nil)
	case errors.Is(err, ErrRunNotFound), errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	default:
		slog.Error("test data request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func projectID(r *http.Request) string {
	if id := chi.URLParam(r, "project_id"); id != "" {
		return id
	}
	return chi.URLParam(r, "id")
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}
