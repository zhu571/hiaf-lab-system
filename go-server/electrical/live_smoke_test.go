package electrical

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// rejectWriteConn makes the passive-device safety boundary executable in the
// live smoke test. Any accidental write to MESTEK or Keithley fails the read.
type rejectWriteConn struct {
	net.Conn
}

func (c *rejectWriteConn) Write(_ []byte) (int, error) {
	return 0, errors.New("passive electrical connection attempted a write")
}

// TestLiveSingleShotSmoke is deliberately opt-in. It queries the configured
// lab devices but writes only to a local httptest Influx endpoint, never to a
// real database. Run with ELECTRICAL_LIVE_SMOKE=1 on the isolated instrument LAN.
func TestLiveSingleShotSmoke(t *testing.T) {
	if os.Getenv("ELECTRICAL_LIVE_SMOKE") != "1" {
		t.Skip("set ELECTRICAL_LIVE_SMOKE=1 to query real instruments")
	}
	writes := make(chan string, 1)
	influx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/write" {
			t.Errorf("unexpected Influx request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Token live-smoke-token" {
			t.Errorf("missing Influx token header")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			t.Errorf("read Influx body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writes <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer influx.Close()

	cfg := DefaultConfig()
	cfg.Enabled = false
	cfg.SingleShotTimeout = 30 * time.Second
	client := NewInfluxClient(InfluxConfig{Addr: influx.URL, Token: "live-smoke-token", Org: "lab", Bucket: "electrical-smoke"})
	svc := NewService(cfg, client)
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	svc.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &rejectWriteConn{Conn: conn}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, err := svc.SingleShot(ctx, "live-smoke")
	if err != nil {
		t.Fatalf("live single-shot: %v (partial result: %+v)", err, result)
	}
	counts := make(map[string]int)
	for _, sample := range result.Samples {
		counts[sample.Source]++
	}
	for _, source := range sourceOrder {
		if counts[source] == 0 {
			t.Errorf("no live samples from %s", source)
		}
	}
	if len(result.Errors) > 0 {
		t.Errorf("live acquisition reported errors: %+v", result.Errors)
	}
	t.Logf("live sample counts: %+v", counts)

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	svc.flushBacklog(writeCtx)
	select {
	case body := <-writes:
		if !strings.Contains(body, "electrical_samples,") {
			t.Fatalf("measurement missing from line protocol: %q", body)
		}
		for _, source := range sourceOrder {
			if !strings.Contains(body, "source="+source+",") {
				t.Errorf("line protocol has no samples for %s", source)
			}
		}
	case <-writeCtx.Done():
		t.Fatal("local Influx endpoint did not receive the batch")
	}
}
