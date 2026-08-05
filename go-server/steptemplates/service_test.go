package steptemplates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReorderSteps(t *testing.T) {
	steps := []StepCandidate{
		{Name: "c", StepOrder: 3, DependsOnOrder: intPtr(1)},
		{Name: "a", StepOrder: 1, DependsOnOrder: nil},
		{Name: "b", StepOrder: 2, DependsOnOrder: intPtr(1)},
	}
	result := reorderSteps(steps)
	if len(result) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result))
	}
	if result[0].Name != "a" || result[0].StepOrder != 1 || result[0].DependsOnOrder != nil {
		t.Fatalf("unexpected step 0: %+v", result[0])
	}
	if result[1].Name != "b" || result[1].StepOrder != 2 || *result[1].DependsOnOrder != 1 {
		t.Fatalf("unexpected step 1: %+v", result[1])
	}
	if result[2].Name != "c" || result[2].StepOrder != 3 || *result[2].DependsOnOrder != 1 {
		t.Fatalf("unexpected step 2: %+v", result[2])
	}
}

func TestValidateSteps(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		steps := []StepCandidate{
			{Name: "a", StepOrder: 1, DependsOnOrder: nil},
			{Name: "b", StepOrder: 2, DependsOnOrder: intPtr(1)},
		}
		if err := validateSteps(steps); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("duplicate order", func(t *testing.T) {
		steps := []StepCandidate{
			{Name: "a", StepOrder: 1, DependsOnOrder: nil},
			{Name: "b", StepOrder: 1, DependsOnOrder: intPtr(1)},
		}
		if err := validateSteps(steps); err == nil {
			t.Fatal("expected error for duplicate order")
		}
	})
	t.Run("zero order", func(t *testing.T) {
		steps := []StepCandidate{
			{Name: "a", StepOrder: 0, DependsOnOrder: nil},
		}
		if err := validateSteps(steps); err == nil {
			t.Fatal("expected error for zero order")
		}
	})
	t.Run("invalid dependency", func(t *testing.T) {
		steps := []StepCandidate{
			{Name: "a", StepOrder: 1, DependsOnOrder: intPtr(99)},
			{Name: "b", StepOrder: 2, DependsOnOrder: nil},
		}
		if err := validateSteps(steps); err == nil {
			t.Fatal("expected error for invalid dependency")
		}
	})
	t.Run("depends on later step", func(t *testing.T) {
		steps := []StepCandidate{
			{Name: "a", StepOrder: 2, DependsOnOrder: intPtr(3)},
			{Name: "b", StepOrder: 3, DependsOnOrder: nil},
		}
		if err := validateSteps(steps); err == nil {
			t.Fatal("expected error for depends on later step")
		}
	})
	t.Run("too many steps", func(t *testing.T) {
		steps := make([]StepCandidate, MaxItems+1)
		for i := range steps {
			steps[i] = StepCandidate{Name: "x", StepOrder: i + 1}
		}
		if err := validateSteps(steps); err == nil {
			t.Fatal("expected error for too many steps")
		}
	})
}

func TestReorderAndNormalizeItems(t *testing.T) {
	items := []ItemDef{
		{Name: "b", StepOrder: 5, DependsOnOrder: intPtr(1)},
		{Name: "a", StepOrder: 1, DependsOnOrder: nil},
	}
	result := reorderAndNormalizeItems(items)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].Name != "a" || result[0].StepOrder != 1 || result[0].DependsOnOrder != nil {
		t.Fatalf("unexpected item 0: %+v", result[0])
	}
	if result[1].Name != "b" || result[1].StepOrder != 2 || *result[1].DependsOnOrder != 1 {
		t.Fatalf("unexpected item 1: %+v", result[1])
	}
}

func TestRequireWriteAccess(t *testing.T) {
	svc := &Service{}
	creatorID := "creator-1"

	t.Run("admin can write", func(t *testing.T) {
		if err := svc.requireWriteAccess("admin", "other", nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("maintainer can write", func(t *testing.T) {
		if err := svc.requireWriteAccess("maintainer", "other", nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("creator can write own", func(t *testing.T) {
		if err := svc.requireWriteAccess("viewer", creatorID, &StepTemplate{CreatedBy: &creatorID}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("viewer cannot write others", func(t *testing.T) {
		if err := svc.requireWriteAccess("viewer", "other", &StepTemplate{CreatedBy: &creatorID}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGenerateNilContextSerializedAsEmptyObject(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","name_suggestion":"t","model":"m","steps":[{"name":"a","step_order":1}]}`))
	}))
	defer server.Close()

	svc := NewService(nil, nil)
	svc.ConfigurePlanner(server.URL, "token")

	_, err := svc.Generate(context.Background(), "user-1", "member", GenerateRequest{
		Kind:    "assembly",
		Prompt:  "装一个靶室",
		Context: nil,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	ctxValue, ok := payload["context"]
	if !ok {
		t.Fatal("payload missing context key")
	}
	ctxMap, ok := ctxValue.(map[string]any)
	if !ok {
		t.Fatalf("context should be an object, got %T (%v)", ctxValue, ctxValue)
	}
	if len(ctxMap) != 0 {
		t.Fatalf("expected empty context, got %v", ctxMap)
	}
	if strings.Contains(string(gotBody), `"context":null`) {
		t.Fatalf("context must not be null: %s", gotBody)
	}
}

func TestGenerateUpstreamErrorMarked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request"}`))
	}))
	defer server.Close()

	svc := NewService(nil, nil)
	svc.ConfigurePlanner(server.URL, "token")

	_, err := svc.Generate(context.Background(), "user-1", "member", GenerateRequest{
		Kind:   "experiment",
		Prompt: "做一次束流实验",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("expected ErrUpstream, got %v", err)
	}
}

func intPtr(v int) *int { return &v }
