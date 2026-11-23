package electrical

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInfluxWriteLineProtocolAndHistory(t *testing.T) {
	var mu sync.Mutex
	var writeBody, fluxBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Token test-token" {
			t.Errorf("missing Influx auth: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v2/write":
			writeBody = string(body)
			if r.URL.Query().Get("precision") != "ns" || r.URL.Query().Get("org") != "lab" || r.URL.Query().Get("bucket") != "electrical" {
				t.Errorf("write query: %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/v2/query":
			fluxBody = string(body)
			w.Header().Set("Content-Type", "text/csv")
			_, _ = io.WriteString(w, "#datatype,dateTime:RFC3339,string,string,string,string,string,string,double,string,string,string,long\n")
			_, _ = io.WriteString(w, "_time,source,channel,metric,unit,mapping,signature,value,text_value,raw,quality,seq\n")
			_, _ = io.WriteString(w, "2026-11-17T03:00:00.123456789Z,rigol_dho814,CH1,vpp_V,V,direct,,1.25,,1.25,good,7\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewInfluxClient(InfluxConfig{Addr: server.URL, Token: "test-token", Org: "lab", Bucket: "electrical"})
	value := 1.25
	at := time.Unix(1, 234).UTC()
	sample := Sample{
		Time: at, Source: SourceRIGOL, Channel: "CH1", Metric: "vpp_V", Unit: "V",
		Mapping: MappingDirect, Value: &value, Raw: "1.25", Quality: "good", Seq: 7,
	}
	if err := client.WriteBatch(context.Background(), []Sample{sample}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"electrical_samples,source=rigol_dho814,channel=CH1,metric=vpp_V,unit=V,mapping=direct,signature=",
		"value=1.25", `quality="good"`, "seq=7i", "1000000234\n",
	} {
		if !strings.Contains(writeBody, expected) {
			t.Errorf("line protocol missing %q: %s", expected, writeBody)
		}
	}
	if strings.Contains(writeBody, "frame_seq") {
		t.Fatalf("frame sequence became a tag: %s", writeBody)
	}

	points, err := client.History(context.Background(), HistoryParams{Source: SourceRIGOL, From: "-1h", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Value == nil || *points[0].Value != 1.25 || points[0].Seq != 7 {
		t.Fatalf("history points: %+v", points)
	}
	for _, expected := range []string{`r["source"] == "rigol_dho814"`, "range(start: -1h", "limit(n: 50)", "pivot("} {
		if !strings.Contains(fluxBody, expected) {
			t.Errorf("Flux missing %q: %s", expected, fluxBody)
		}
	}
}

func TestInfluxHistoryValidation(t *testing.T) {
	client := NewInfluxClient(InfluxConfig{Addr: "127.0.0.1:1"})
	if _, err := client.History(context.Background(), HistoryParams{From: `1h" or true`}); err == nil {
		t.Fatal("unsafe from accepted")
	}
	if _, err := client.History(context.Background(), HistoryParams{Source: "rigol_dho814", Limit: 5001}); err == nil {
		t.Fatal("unbounded history accepted")
	}
}
