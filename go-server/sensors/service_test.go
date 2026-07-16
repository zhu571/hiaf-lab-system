package sensors

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCSVParsesFloatValues(t *testing.T) {
	body := []byte(`#datatype,string,long,dateTime:RFC3339,string,double
result,table,_time,tag,_value
,0,2026-07-15T10:00:00+08:00,pressure,1.23e-4
,0,2026-07-15T10:01:00+08:00,pressure,not-a-number
`)

	points := parseCSV(body)
	if len(points) != 1 {
		t.Fatalf("expected 1 parsed point, got %d: %+v", len(points), points)
	}
	if points[0].Tag != "pressure" || points[0].Value != 1.23e-4 {
		t.Fatalf("unexpected point: %+v", points[0])
	}
}

func TestLatestBuildsORFilterForMultipleMeasurements(t *testing.T) {
	var gotFlux string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotFlux = string(body)
		w.Write([]byte("result,table,_time,tag,_value\n,0,2026-07-15T10:00:00+08:00,pressure,1\n"))
	}))
	defer server.Close()

	svc := NewServiceWithConfig(Config{
		Addr:         server.URL,
		Token:        "test-token",
		Org:          "lab-org",
		Bucket:       "lab-bucket",
		Measurements: []string{"pressure", "vacuum"},
	})
	if _, err := svc.Latest("pressure,vacuum"); err != nil {
		t.Fatalf("Latest returned error: %v", err)
	}
	if !strings.Contains(gotFlux, `r["_measurement"] == "pressure" or r["_measurement"] == "vacuum"`) {
		t.Fatalf("expected OR measurement filter, got:\n%s", gotFlux)
	}
}
