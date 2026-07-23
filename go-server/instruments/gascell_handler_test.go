package instruments

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGasCellSSEFrame(t *testing.T) {
	h := NewHandler(NewServiceWithGateway("http://unused"))
	recorder := httptest.NewRecorder()
	if !h.writeSSE(recorder, 1, "snapshot", map[string]PVPoint{"GasCell:Piezo:A1": {Value: 1.2, Quality: "good"}}) {
		t.Fatal("writeSSE failed")
	}
	body := recorder.Body.String()
	for _, want := range []string{"id: 1\n", `"type":"snapshot"`, `"seq":1`, `"epoch":`, `"GasCell:Piezo:A1":{"v":1.2,"q":"good"}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE frame %q does not contain %q", body, want)
		}
	}
}
