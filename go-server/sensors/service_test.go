package sensors

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSafeFluxValue(t *testing.T) {
	valid := []string{"now()", "-1h", "30m", "1d", "12", "0"}
	for _, v := range valid {
		if err := safeFluxValue(v); err != nil {
			t.Errorf("safeFluxValue(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"", "abc", "1h; drop bucket", `1h" or true`, "-1.5h", "1.5x", "-", "h", "1e3", "now"}
	for _, v := range invalid {
		if err := safeFluxValue(v); err == nil {
			t.Errorf("safeFluxValue(%q) = nil, want error", v)
		}
	}
}

func TestSafeTag(t *testing.T) {
	valid := []string{"pressure", "pressure-1", "Pump_2", "真空"}
	for _, v := range valid {
		if err := safeTag(v); err != nil {
			t.Errorf("safeTag(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{`p"1`, `p\1`, "p\n1", "p\r1", "p\\n1", "p\\r1"}
	for _, v := range invalid {
		if err := safeTag(v); err == nil {
			t.Errorf("safeTag(%q) = nil, want error", v)
		}
	}
}

func TestEnvList(t *testing.T) {
	t.Run("env set", func(t *testing.T) {
		t.Setenv("SENSORS_TEST_MEAS", "a, b ,c")
		got := envList("SENSORS_TEST_MEAS", defaultMeasurements)
		if len(got) != 3 || got[1] != " b " {
			t.Fatalf("envList = %v", got)
		}
	})
	t.Run("env unset uses default", func(t *testing.T) {
		os.Unsetenv("SENSORS_TEST_MEAS")
		got := envList("SENSORS_TEST_MEAS", defaultMeasurements)
		if len(got) != len(defaultMeasurements) {
			t.Fatalf("envList default = %v", got)
		}
	})
}

func TestNormalizeHTTPBase(t *testing.T) {
	cases := map[string]string{
		"influx:8086":     "http://influx:8086",
		"influx:8086/":    "http://influx:8086",
		"http://a:8086/":  "http://a:8086",
		"https://a:8086/": "https://a:8086",
	}
	for in, want := range cases {
		if got := normalizeHTTPBase(in); got != want {
			t.Errorf("normalizeHTTPBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewService(t *testing.T) {
	// /run/secrets/influxdb_token 存在时优先读文件，跳过本环境断言。
	if _, err := os.Stat("/run/secrets/influxdb_token"); err == nil {
		t.Skip("/run/secrets/influxdb_token exists; env fallback untestable here")
	}
	t.Run("missing token", func(t *testing.T) {
		old, had := os.LookupEnv("INFLUXDB_TOKEN")
		os.Unsetenv("INFLUXDB_TOKEN")
		t.Cleanup(func() {
			if had {
				os.Setenv("INFLUXDB_TOKEN", old)
			}
		})
		if _, err := NewService(); err == nil {
			t.Fatal("expected error for missing token")
		}
	})
	t.Run("missing addr/org/bucket", func(t *testing.T) {
		t.Setenv("INFLUXDB_TOKEN", "tok")
		os.Unsetenv("INFLUXDB_ADDR")
		os.Unsetenv("INFLUXDB_ORG")
		os.Unsetenv("INFLUXDB_BUCKET")
		if _, err := NewService(); err == nil {
			t.Fatal("expected error for missing addr/org/bucket")
		}
	})
	t.Run("configured", func(t *testing.T) {
		t.Setenv("INFLUXDB_TOKEN", "tok")
		t.Setenv("INFLUXDB_ADDR", "influx:8086")
		t.Setenv("INFLUXDB_ORG", "lab")
		t.Setenv("INFLUXDB_BUCKET", "sensors")
		svc, err := NewService()
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		if svc.addr != "http://influx:8086" || svc.token != "tok" || svc.bucket != "sensors" {
			t.Fatalf("service not configured: %+v", svc)
		}
		if !svc.measurements["pressure"] || !svc.measurements["pump"] {
			t.Fatalf("default measurements missing: %v", svc.measurements)
		}
	})
}

// mockInflux 返回一个记录请求并回放预设响应的 InfluxDB v2 API mock。
func mockInflux(t *testing.T, status int, body string, hook func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil {
			hook(r)
		}
		w.Header().Set("Content-Type", "application/csv")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func newMockSvc(t *testing.T, serverURL string) *Service {
	t.Helper()
	return NewServiceWithConfig(Config{
		Addr:         serverURL,
		Token:        "tok",
		Org:          "lab",
		Bucket:       "sensors",
		Measurements: defaultMeasurements,
	})
}

func TestQueryInflux(t *testing.T) {
	var gotAuth, gotCT, gotOrg string
	server := mockInflux(t, 200, "#datatype,string\ntest,ok", func(r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotOrg = r.URL.Query().Get("org")
	})
	defer server.Close()
	svc := newMockSvc(t, server.URL)

	body, err := svc.queryInflux(`from(bucket: "sensors")`)
	if err != nil {
		t.Fatalf("queryInflux: %v", err)
	}
	if !strings.Contains(string(body), "test,ok") {
		t.Fatalf("body = %q", body)
	}
	if gotAuth != "Token tok" || gotCT != "application/vnd.flux" || gotOrg != "lab" {
		t.Fatalf("request headers/query: auth=%q ct=%q org=%q", gotAuth, gotCT, gotOrg)
	}

	// 非 2xx → 错误带状态码
	serverErr := mockInflux(t, 500, "boom", nil)
	defer serverErr.Close()
	svcErr := newMockSvc(t, serverErr.URL)
	if _, err := svcErr.queryInflux("x"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}

	// 连接失败 → 错误
	badSvc := newMockSvc(t, "http://127.0.0.1:1")
	if _, err := badSvc.queryInflux("x"); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestLatest(t *testing.T) {
	csv := "#group,false,false\n_time,tag,_value\n2026-08-01T10:00:00Z,pressure,1.5\n"
	t.Run("with tags", func(t *testing.T) {
		var fluxBody string
		server := mockInflux(t, 200, csv, func(r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			fluxBody = string(b)
		})
		defer server.Close()
		svc := newMockSvc(t, server.URL)

		res, err := svc.Latest("pressure, vacuum")
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if len(res.Points) != 1 || res.Points[0].Tag != "pressure" || res.Points[0].Value != 1.5 {
			t.Fatalf("points: %+v", res.Points)
		}
		if !strings.Contains(fluxBody, `r["_measurement"] == "pressure" or r["_measurement"] == "vacuum"`) {
			t.Fatalf("measurement filter missing: %s", fluxBody)
		}
		if !strings.Contains(fluxBody, "range(start: -1h)") {
			t.Fatalf("default range missing: %s", fluxBody)
		}
	})
	t.Run("no tags means all measurements", func(t *testing.T) {
		var fluxBody string
		server := mockInflux(t, 200, "", func(r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			fluxBody = string(b)
		})
		defer server.Close()
		svc := newMockSvc(t, server.URL)
		if _, err := svc.Latest(""); err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if strings.Contains(fluxBody, "_measurement") {
			t.Fatalf("unexpected filter: %s", fluxBody)
		}
	})
	t.Run("unknown measurement", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.Latest("nonsense"); err == nil || !strings.Contains(err.Error(), "unknown measurement") {
			t.Fatalf("expected unknown measurement error, got %v", err)
		}
	})
	t.Run("unsafe measurement", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.Latest(`pressure"`); err == nil {
			t.Fatal("expected invalid measurement error")
		}
	})
}

func TestHistory(t *testing.T) {
	csv := "_time,tag,_value\n2026-08-01T10:00:00Z,pressure,2.5\n"
	t.Run("defaults and interval", func(t *testing.T) {
		var fluxBody string
		server := mockInflux(t, 200, csv, func(r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			fluxBody = string(b)
		})
		defer server.Close()
		svc := newMockSvc(t, server.URL)

		res, err := svc.History("pressure", "", "", "5m")
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		if len(res.Points) != 1 || res.Points[0].Value != 2.5 {
			t.Fatalf("points: %+v", res.Points)
		}
		if !strings.Contains(fluxBody, "range(start: -1h, stop: now())") {
			t.Fatalf("default range missing: %s", fluxBody)
		}
		if !strings.Contains(fluxBody, "aggregateWindow(every: 5m") {
			t.Fatalf("aggregateWindow missing: %s", fluxBody)
		}
	})
	t.Run("custom from/to", func(t *testing.T) {
		var fluxBody string
		server := mockInflux(t, 200, "", func(r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			fluxBody = string(b)
		})
		defer server.Close()
		svc := newMockSvc(t, server.URL)
		if _, err := svc.History("pressure", "-2h", "-1h", ""); err != nil {
			t.Fatalf("History: %v", err)
		}
		if !strings.Contains(fluxBody, "range(start: -2h, stop: -1h)") {
			t.Fatalf("custom range missing: %s", fluxBody)
		}
	})
	t.Run("unknown tag", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.History("nonsense", "", "", ""); err == nil {
			t.Fatal("expected unknown measurement error")
		}
	})
	t.Run("unsafe tag", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.History(`p"`, "", "", ""); err == nil {
			t.Fatal("expected invalid tag error")
		}
	})
	t.Run("invalid from", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.History("pressure", "abc", "", ""); err == nil {
			t.Fatal("expected invalid from error")
		}
	})
	t.Run("invalid to", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.History("pressure", "", "1.5h;x", ""); err == nil {
			t.Fatal("expected invalid to error")
		}
	})
	t.Run("invalid interval", func(t *testing.T) {
		svc := newMockSvc(t, "")
		if _, err := svc.History("pressure", "", "", "every 5m"); err == nil {
			t.Fatal("expected invalid interval error")
		}
	})
	t.Run("upstream error surfaces", func(t *testing.T) {
		server := mockInflux(t, 503, "unavailable", nil)
		defer server.Close()
		svc := newMockSvc(t, server.URL)
		if _, err := svc.History("pressure", "", "", ""); err == nil {
			t.Fatal("expected upstream error")
		}
	})
}

func TestParseCSV(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		if points := parseCSV(nil); points != nil {
			t.Fatalf("expected nil, got %v", points)
		}
	})
	t.Run("header only", func(t *testing.T) {
		if points := parseCSV([]byte("_time,tag,_value\n")); points != nil {
			t.Fatalf("expected nil, got %v", points)
		}
	})
	t.Run("comment lines skipped", func(t *testing.T) {
		body := []byte("#datatype,string,long\n#group,false,false\n_time,tag,_value\n2026-01-01T00:00:00Z,a,1.0\n")
		points := parseCSV(body)
		if len(points) != 1 || points[0].Tag != "a" || points[0].Value != 1.0 {
			t.Fatalf("points: %+v", points)
		}
	})
	t.Run("missing value column", func(t *testing.T) {
		if points := parseCSV([]byte("_time,tag\n2026-01-01T00:00:00Z,a\n")); points != nil {
			t.Fatalf("expected nil, got %v", points)
		}
	})
	t.Run("quoted field with comma", func(t *testing.T) {
		body := []byte("_time,tag,_value\n2026-01-01T00:00:00Z,\"a,1\",3.5\n")
		points := parseCSV(body)
		if len(points) != 1 || points[0].Tag != "a,1" || points[0].Value != 3.5 {
			t.Fatalf("points: %+v", points)
		}
	})
	t.Run("bad float skipped", func(t *testing.T) {
		body := []byte("_time,tag,_value\n2026-01-01T00:00:00Z,a,abc\n")
		if points := parseCSV(body); len(points) != 0 {
			t.Fatalf("expected empty, got %v", points)
		}
	})
	t.Run("nan and inf skipped", func(t *testing.T) {
		body := []byte("_time,tag,_value\n2026-01-01T00:00:00Z,a,NaN\n2026-01-01T00:00:01Z,b,+Inf\n2026-01-01T00:00:02Z,c,-Inf\n2026-01-01T00:00:03Z,d,1.5\n")
		points := parseCSV(body)
		if len(points) != 1 || points[0].Value != 1.5 {
			t.Fatalf("non-finite values must be skipped: %+v", points)
		}
	})
	t.Run("short row skipped", func(t *testing.T) {
		body := []byte("_time,tag,_value\n2026-01-01T00:00:00Z\n")
		if points := parseCSV(body); len(points) != 0 {
			t.Fatalf("expected empty, got %v", points)
		}
	})
}

func TestSplitCSVLine(t *testing.T) {
	cols := splitCSVLine(`a,"b,c",d`)
	if len(cols) != 3 || cols[0] != "a" || cols[1] != "b,c" || cols[2] != "d" {
		t.Fatalf("cols: %v", cols)
	}
	cols = splitCSVLine("a,,c")
	if len(cols) != 3 || cols[1] != "" {
		t.Fatalf("empty col: %v", cols)
	}
}
