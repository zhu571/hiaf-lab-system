package testdata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

func TestDecodeUpdateRequestRejectsImmutableFields(t *testing.T) {
	request := httptest.NewRequest("PATCH", "/api/v1/test-data/id", strings.NewReader(`{"run_id":"x"}`))
	if _, err := decodeUpdateRequest(request); err == nil {
		t.Fatal("decodeUpdateRequest accepted immutable run_id")
	}
}

func newBatchRouter(svc *Service) http.Handler {
	middleware.SetJWTSecret([]byte("batch-test-secret"))
	router := chi.NewRouter()
	router.Route("/api/v1/projects/{project_id}", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Post("/test-data/batch", (&Handler{svc: svc}).CreateBatch)
	})
	return router
}

func batchRequest(router http.Handler, body string, withKey bool) *httptest.ResponseRecorder {
	token, err := middleware.GenerateToken("tester", "tester", auth.RoleMember, 1, []byte("batch-test-secret"))
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectUUID+"/test-data/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if withKey {
		req.Header.Set("Idempotency-Key", "batch-test-key")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type errorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

func TestCreateBatchValidation422Shape(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	body := `[
		{"data_type": "quantum", "measurement": "t", "value": 1},
		{"data_type": "cryo", "measurement": "t"},
		{"data_type": "cryo", "measurement": "t", "value": 1, "run_id": "not-a-uuid"}
	]`
	rec := batchRequest(router, body, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "validation_failed" {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
	if !strings.Contains(envelope.Error.Message, "3 行校验失败") {
		t.Fatalf("message = %q", envelope.Error.Message)
	}
	raw, ok := envelope.Error.Details["errors"]
	if !ok {
		t.Fatal("details 缺 errors")
	}
	items, ok := raw.([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("errors = %v, want 3 条", raw)
	}
	first := items[0].(map[string]any)
	for _, key := range []string{"index", "field", "code", "message"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("errors[0] 缺字段 %s：%v", key, first)
		}
	}
	if first["index"].(float64) != 0 || first["field"] != "data_type" || first["code"] != "invalid_enum" {
		t.Fatalf("errors[0] = %v", first)
	}
}

func TestCreateBatchTooLarge422(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	rows := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		rows = append(rows, `{"data_type":"cryo","measurement":"t","value":1}`)
	}
	rec := batchRequest(router, "["+strings.Join(rows, ",")+"]", true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "batch_too_large" {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
	if envelope.Error.Details["max"].(float64) != 100 || envelope.Error.Details["received"].(float64) != 101 {
		t.Fatalf("details = %v", envelope.Error.Details)
	}
}

func TestCreateBatchUnknownFieldRowError(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	body := `[
		{"data_type": "cryo", "measurement": "t", "value": 1, "sneaky": true},
		{"data_type": "quantum", "measurement": "t", "value": 2}
	]`
	rec := batchRequest(router, body, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	raw := envelope.Error.Details["errors"].([]any)
	// 解码失败行只报 unknown_field（不再叠加占位空行补出的 required）；其余行继续语义校验（invalid_enum）。
	if len(raw) != 2 {
		t.Fatalf("errors = %v, want 2（解码失败行只报 unknown_field，其余行继续校验）", raw)
	}
	first := raw[0].(map[string]any)
	if first["index"].(float64) != 0 || first["field"] != "sneaky" || first["code"] != "unknown_field" {
		t.Fatalf("errors[0] = %v, want unknown_field/sneaky", first)
	}
	last := raw[1].(map[string]any)
	if last["index"].(float64) != 1 || last["field"] != "data_type" || last["code"] != "invalid_enum" {
		t.Fatalf("errors[1] = %v", last)
	}
}

func TestCreateBatchBodyTooLarge413(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	// 100 行 × ~6KB notes ≈ 600KB，超过 512KB 上限但低于行数上限，触发 MaxBytesReader 而非 batch_too_large。
	bigRow := `{"data_type":"cryo","measurement":"t","value":1,"notes":"` + strings.Repeat("x", 6<<10) + `"}`
	rows := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		rows = append(rows, bigRow)
	}
	rec := batchRequest(router, "["+strings.Join(rows, ",")+"]", true)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413，body = %s", rec.Code, rec.Body.String())
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "request_too_large" {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
	if envelope.Error.Details["max"].(float64) != batchMaxBodyBytes {
		t.Fatalf("details = %v", envelope.Error.Details)
	}
}

func TestCreateBatchNonArray400(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	for name, body := range map[string]string{
		"object": `{"data_type":"cryo"}`,
		"null":   `null`,
		"empty":  `[]`,
	} {
		rec := batchRequest(router, body, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, rec.Code)
		}
		var envelope errorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != "bad_request" {
			t.Fatalf("%s: code = %q", name, envelope.Error.Code)
		}
	}
}

func TestCreateBatchMissingIdempotencyKey(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	router := newBatchRouter(svc)
	rec := batchRequest(router, `[{"data_type":"cryo","measurement":"t","value":1}]`, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "missing_idempotency_key" {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
}

func TestCreateBatchSuccess201(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})
	router := newBatchRouter(svc)
	runID := runUUID
	body := `[
		{"data_type": "cryo", "measurement": "target_temp", "value": 4.2, "unit": "K", "run_id": "` + runID + `"},
		{"data_type": "pressure", "measurement": "cell_pressure", "value": 0.013}
	]`
	rec := batchRequest(router, body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201，body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Count int `json:"count"`
			Items []struct {
				ID          string  `json:"id"`
				ProjectID   string  `json:"project_id"`
				DataType    string  `json:"data_type"`
				Measurement string  `json:"measurement"`
				Value       float64 `json:"value"`
				Quality     string  `json:"quality"`
				Source      string  `json:"source"`
			} `json:"items"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Count != 2 || len(envelope.Data.Items) != 2 {
		t.Fatalf("data = %+v", envelope.Data)
	}
	first := envelope.Data.Items[0]
	if first.ID == "" || first.ProjectID != projectUUID || first.DataType != DataTypeCryo ||
		first.Measurement != "target_temp" || first.Value != 4.2 || first.Quality != QualityNormal || first.Source != SourceManual {
		t.Fatalf("items[0] = %+v", first)
	}
	if repo.batchCalls != 1 {
		t.Fatalf("CreateBatch calls = %d, want 1", repo.batchCalls)
	}
}
