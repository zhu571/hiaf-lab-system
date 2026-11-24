package electrical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func newStateTestService(t *testing.T, cfg Config) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewService(cfg, nil)
	svc.repo = NewRepository(db)
	return svc, mock
}

func healthFor(t *testing.T, svc *Service, source string) SourceHealth {
	t.Helper()
	for _, health := range svc.Latest().Sources {
		if health.Source == source {
			return health
		}
	}
	t.Fatalf("source %q not present in health", source)
	return SourceHealth{}
}

func TestLoadSourceStatesMergesPersistedAndDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	cfg.MESTEKEnabled = false
	svc, mock := newStateTestService(t, cfg)

	rows := sqlmock.NewRows([]string{"source", "enabled"}).
		AddRow(SourceMESTEK, true).
		AddRow(Source33512B, false)
	mock.ExpectQuery("SELECT source, enabled FROM electrical_source_states").WillReturnRows(rows)
	mock.ExpectExec("INSERT INTO electrical_source_states").
		WithArgs(Source6485, true).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO electrical_source_states").
		WithArgs(Source33210A, true).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := svc.loadSourceStates(context.Background()); err != nil {
		t.Fatalf("loadSourceStates: %v", err)
	}
	if !svc.sourceEnabled(SourceMESTEK) {
		t.Fatal("persisted mestek=true did not override env default false")
	}
	if svc.sourceEnabled(Source33512B) {
		t.Fatal("persisted agilent_33512b=false was not applied")
	}
	if !svc.sourceEnabled(Source6485) || !svc.sourceEnabled(Source33210A) {
		t.Fatal("missing sources did not keep their enabled defaults")
	}
	if health := healthFor(t, svc, Source33512B); health.Enabled || health.Status != "disabled" {
		t.Fatalf("33512b health = %+v, want enabled=false status=disabled", health)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetSourceEnabledPersistsAndUpdatesHealth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc, mock := newStateTestService(t, cfg)
	mock.ExpectExec("INSERT INTO electrical_source_states").
		WithArgs(Source33210A, false).WillReturnResult(sqlmock.NewResult(1, 1))

	health, err := svc.SetSourceEnabled(context.Background(), Source33210A, false)
	if err != nil {
		t.Fatalf("SetSourceEnabled: %v", err)
	}
	if health.Enabled || health.Status != "disabled" {
		t.Fatalf("returned health = %+v, want disabled", health)
	}
	if svc.sourceEnabled(Source33210A) {
		t.Fatal("in-memory switch was not updated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetSourceEnabledRejectsUnknownSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	if _, err := svc.SetSourceEnabled(context.Background(), SourceRIGOL, false); err == nil {
		t.Fatal("non-toggleable source should be rejected")
	}
}

func TestSetSourceEnabledHandlerAuthorization(t *testing.T) {
	oldSecret := middleware.JWTSecret
	middleware.SetJWTSecret([]byte("test-secret"))
	t.Cleanup(func() { middleware.JWTSecret = oldSecret })

	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	handler := NewHandler(svc)
	router := chi.NewRouter()
	router.Use(middleware.AuthRequired)
	router.Post("/api/v1/electrical/sources/{source}/enabled", handler.SetSourceEnabled)

	token := func(role string) string {
		signed, err := middleware.GenerateToken("user-1", "tester", role, 0, middleware.JWTSecret)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		return signed
	}
	serve := func(role, source, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/electrical/sources/"+source+"/enabled", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if role != "" {
			request.Header.Set("Authorization", "Bearer "+token(role))
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	if recorder := serve("", SourceMESTEK, `{"enabled":false}`); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d, want 401", recorder.Code)
	}
	if recorder := serve("member", SourceMESTEK, `{"enabled":false}`); recorder.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", recorder.Code)
	}
	if recorder := serve("admin", "rigol_dho814", `{"enabled":false}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown source status = %d, want 400", recorder.Code)
	}
	if recorder := serve("admin", SourceMESTEK, `{}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, want 400", recorder.Code)
	}

	recorder := serve("admin", SourceMESTEK, `{"enabled":false}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data SourceHealth `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Enabled || body.Data.Status != "disabled" {
		t.Fatalf("response health = %+v, want disabled", body.Data)
	}
}

func disableWorkers(cfg *Config) {
	cfg.Enabled = true
	cfg.Devices = []DeviceConfig{{Source: Source33512B, Addr: "fake:5025"}}
	cfg.MESTEKEnabled = false
	cfg.KeithleyEnabled = false
	cfg.QueryInterval = 10 * time.Millisecond
	cfg.MESTEKReadInterval = 5 * time.Millisecond
	cfg.KeithleyReadInterval = 5 * time.Millisecond
	cfg.ReconnectDelay = 5 * time.Millisecond
}

func countDeviceQueries(svc *Service) *atomic.Int64 {
	var calls atomic.Int64
	for _, device := range svc.devices {
		device.query = func(context.Context, string, string) (string, error) {
			calls.Add(1)
			return "", errors.New("offline")
		}
	}
	return &calls
}

func runFor(t *testing.T, svc *Service, duration time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	time.Sleep(duration)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("service did not stop")
	}
}

func TestDisabledSourceSkipsCollection(t *testing.T) {
	cfg := DefaultConfig()
	disableWorkers(&cfg)
	svc := NewService(cfg, nil)
	if _, err := svc.SetSourceEnabled(context.Background(), Source33512B, false); err != nil {
		t.Fatal(err)
	}
	calls := countDeviceQueries(svc)
	runFor(t, svc, 60*time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("disabled source issued %d queries", calls.Load())
	}
	if health := healthFor(t, svc, Source33512B); health.Enabled || health.Status != "disabled" {
		t.Fatalf("disabled source health = %+v, want disabled", health)
	}
}

func TestReenabledSourceResumesCollection(t *testing.T) {
	cfg := DefaultConfig()
	disableWorkers(&cfg)
	svc := NewService(cfg, nil)
	if _, err := svc.SetSourceEnabled(context.Background(), Source33512B, false); err != nil {
		t.Fatal(err)
	}
	calls := countDeviceQueries(svc)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()

	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("disabled source issued %d queries before being enabled", calls.Load())
	}
	if _, err := svc.SetSourceEnabled(ctx, Source33512B, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("re-enabled source never resumed collection")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !healthFor(t, svc, Source33512B).Enabled {
		t.Fatal("health did not reflect the re-enabled source")
	}
}
