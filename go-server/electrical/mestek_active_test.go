package electrical

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeMESTEKServer struct {
	dial        dialContextFunc
	connections *atomic.Int64
	requests    chan []byte
	wait        func()
}

func newFakeMESTEKServer(t *testing.T, handler func([]byte, int64) []byte) fakeMESTEKServer {
	t.Helper()
	var connections atomic.Int64
	var wg sync.WaitGroup
	requests := make(chan []byte, 100)
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		connection := connections.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer server.Close()
			// A real snapshot reuses one connection for the whole round, so the
			// fake must keep serving requests until the client closes the socket.
			for {
				request := make([]byte, 8)
				if _, err := io.ReadFull(server, request); err != nil {
					return
				}
				requestCopy := append([]byte(nil), request...)
				requests <- requestCopy
				if response := handler(requestCopy, connection); len(response) > 0 {
					if _, err := server.Write(response); err != nil {
						return
					}
				}
			}
		}()
		return client, nil
	}
	return fakeMESTEKServer{
		dial: dial, connections: &connections, requests: requests,
		wait: func() {
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("fake MESTEK server did not stop")
			}
		},
	}
}

func fakeMESTEKResponse(t *testing.T, request []byte) []byte {
	t.Helper()
	if len(request) != 8 || request[1] != 0x03 || !ValidCRC(request) {
		t.Errorf("invalid active request: %x", request)
		return nil
	}
	unit := request[0]
	start := binary.BigEndian.Uint16(request[2:4])
	count := binary.BigEndian.Uint16(request[4:6])
	if !validMESTEKRead(unit, start, count) {
		t.Errorf("request escaped fixed read map: %x", request)
		return nil
	}
	if start == 0 {
		channels, station := uint16(3), uint16(0)
		online := uint16(0x7)
		if unit == 0x15 {
			channels, station, online = 10, 21, 0x3ff
		}
		return testResponse(unit, []uint16{0, channels, online, station, 0})
	}
	registers := make([]uint16, 16)
	registers[0] = 1
	registers[4] = 3200
	registers[5] = 5100
	registers[6] = uint16(start / 10)
	registers[7] = uint16(start)
	registers[10] = uint16(start / 20)
	registers[12] = 6000
	registers[14] = 100
	return testResponse(unit, registers)
}

func drainMESTEKRequests(t *testing.T, requests <-chan []byte, count int) [][]byte {
	t.Helper()
	result := make([][]byte, 0, count)
	for len(result) < count {
		select {
		case request := <-requests:
			result = append(result, request)
		case <-time.After(time.Second):
			t.Fatalf("received %d/%d requests", len(result), count)
		}
	}
	return result
}

func TestMESTEKActiveSnapshotMapsAllThirteenChannels(t *testing.T) {
	var served atomic.Int64
	server := newFakeMESTEKServer(t, func(request []byte, _ int64) []byte {
		response := fakeMESTEKResponse(t, request)
		if served.Add(1) != 4 {
			return response
		}
		unit := request[0]
		otherUnit := byte(0x15)
		if unit == 0x15 {
			otherUnit = 0x14
		}
		wrongUnit := testResponse(otherUnit, make([]uint16, 16))
		wrongLength := testResponse(unit, make([]uint16, 20))
		badCRC := testResponse(unit, make([]uint16, 16))
		badCRC[len(badCRC)-1] ^= 0xff
		combined := append(wrongUnit, wrongLength...)
		combined = append(combined, badCRC...)
		return append(combined, response...)
	})
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	svc.dial = server.dial
	samples, err := svc.collectActiveMESTEK(context.Background())
	server.wait()
	if err != nil {
		t.Fatal(err)
	}
	if server.connections.Load() != 1 {
		t.Fatalf("connections = %d, want 1 reused snapshot connection", server.connections.Load())
	}
	requests := drainMESTEKRequests(t, server.requests, 15)
	for index, request := range requests {
		wantHeader := index == 0 || index == 4
		count := binary.BigEndian.Uint16(request[4:6])
		if (wantHeader && count != 5) || (!wantHeader && count != 16) {
			t.Errorf("request %d count = %d, header=%v", index, count, wantHeader)
		}
	}
	channels := make(map[string][]Sample)
	for _, sample := range samples {
		channels[sample.Channel] = append(channels[sample.Channel], sample)
		if sample.Mapping != MappingPaired || sample.Raw == "" {
			t.Errorf("active sample lost provenance: %+v", sample)
		}
	}
	if len(samples) != 78 || len(channels) != 13 {
		t.Fatalf("samples=%d channels=%d, want 78 samples across 13 channels", len(samples), len(channels))
	}
	for channel := 1; channel <= 13; channel++ {
		name := fmt.Sprintf("CH%d", channel)
		if len(channels[name]) != 6 {
			t.Errorf("%s has %d samples, want 5 values + raw frame", name, len(channels[name]))
		}
	}
	health := svc.Latest().Sources[0]
	if health.Mode != "modbus_active" || health.MappedFrames != 13 || health.CRCErrors != 1 {
		t.Fatalf("unexpected active health: %+v", health)
	}
}

func TestMESTEKActiveSnapshotRetainsRecentlyMissingChannel(t *testing.T) {
	server := newFakeMESTEKServer(t, func(request []byte, _ int64) []byte {
		unit := request[0]
		start := binary.BigEndian.Uint16(request[2:4])
		if unit == 0x15 && start == 400 { // global CH7
			return testResponse(0x14, make([]uint16, 16))
		}
		return fakeMESTEKResponse(t, request)
	})
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	svc.dial = server.dial
	staleCurrent := 0.123
	svc.record([]Sample{{Source: SourceMESTEK, Channel: "CH7", Metric: "I_A", Mapping: MappingPaired, Value: &staleCurrent, Quality: "good"}})
	samples, err := svc.collectActiveMESTEK(context.Background())
	server.wait()
	var snapshotErr *MESTEKSnapshotError
	if err == nil || !strings.Contains(err.Error(), "12/13") || !strings.Contains(err.Error(), "CH7") {
		t.Fatalf("partial snapshot error = %v", err)
	}
	if !errors.As(err, &snapshotErr) || snapshotErr.Mapped != 12 {
		t.Fatalf("typed partial snapshot error = %v", err)
	}
	for _, sample := range samples {
		if sample.Channel == "CH7" || sample.Channel == "UNMAPPED" {
			t.Fatalf("failed channel was fabricated in this round: %+v", sample)
		}
	}
	if _, ok := svc.latest[sampleKey(Sample{Source: SourceMESTEK, Channel: "CH7", Metric: "I_A", Mapping: MappingPaired})]; !ok {
		t.Fatal("recently missing channel was dropped, causing UI jitter")
	}
	if server.connections.Load() != 1 {
		t.Fatalf("connections = %d, want 1 reused snapshot connection", server.connections.Load())
	}
}

func TestMESTEKActivePrunesOldMissingChannel(t *testing.T) {
	server := newFakeMESTEKServer(t, func(request []byte, _ int64) []byte {
		unit := request[0]
		start := binary.BigEndian.Uint16(request[2:4])
		if unit == 0x15 && start == 400 { // global CH7
			return testResponse(0x14, make([]uint16, 16))
		}
		return fakeMESTEKResponse(t, request)
	})
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	svc.dial = server.dial
	old := time.Now().Add(-2 * svc.mestekStaleAfter())
	svc.record([]Sample{{
		Time: old, Source: SourceMESTEK, Channel: "CH7", Metric: "I_A",
		Mapping: MappingPaired, Value: float64Ptr(0.123), Quality: "good",
	}})
	_, err := svc.collectActiveMESTEK(context.Background())
	server.wait()
	if err == nil {
		t.Fatal("expected a partial snapshot error")
	}
	if _, ok := svc.latest[sampleKey(Sample{Source: SourceMESTEK, Channel: "CH7", Metric: "I_A", Mapping: MappingPaired})]; ok {
		t.Fatal("stale missing channel survived past the retention window")
	}
}

func TestMESTEKActiveTimeoutRetriesOnSameConnection(t *testing.T) {
	server := newFakeMESTEKServer(t, func(_ []byte, _ int64) []byte {
		return nil // never answer: every attempt must time out
	})
	cfg := DefaultConfig()
	svc := NewService(cfg, nil)
	svc.dial = server.dial
	snapshot := &mestekSnapshot{service: svc}
	_, err := snapshot.readHolding(context.Background(), 0x14, 100, 16)
	snapshot.close()
	server.wait()
	if err == nil || !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("timeout error = %v", err)
	}
	requests := drainMESTEKRequests(t, server.requests, 3)
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3 same-connection retries", len(requests))
	}
	if server.connections.Load() != 1 {
		t.Fatalf("connections = %d, want 1 connection with same-socket retries", server.connections.Load())
	}
}

func TestMESTEKActiveRecoversFailedChannelAfterSettle(t *testing.T) {
	var ch13Requests atomic.Int64
	server := newFakeMESTEKServer(t, func(request []byte, _ int64) []byte {
		if request[0] == 0x15 && binary.BigEndian.Uint16(request[2:4]) == 1000 {
			// Drop the three sweep attempts so the channel fails the main round,
			// then answer the post-settle recovery read.
			if ch13Requests.Add(1) <= mestekReadRetries {
				return nil
			}
		}
		return fakeMESTEKResponse(t, request)
	})
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	svc.dial = server.dial
	started := time.Now()
	samples, err := svc.collectActiveMESTEK(context.Background())
	elapsed := time.Since(started)
	server.wait()
	if err != nil {
		t.Fatalf("round with recoverable channel returned error: %v", err)
	}
	if server.connections.Load() != 1 {
		t.Fatalf("connections = %d, want 1 reused snapshot connection", server.connections.Load())
	}
	channels := make(map[string]bool)
	for _, sample := range samples {
		if sample.Channel != "UNMAPPED" {
			channels[sample.Channel] = true
		}
	}
	if len(channels) != 13 {
		t.Fatalf("channels = %d, want 13 after settle retry", len(channels))
	}
	if elapsed < mestekRecoveryDelay {
		t.Fatalf("round returned in %v, settle delay was not applied", elapsed)
	}
	if got := ch13Requests.Load(); got != mestekReadRetries+1 {
		t.Fatalf("CH13 requests = %d, want %d (3 sweep attempts + 1 recovery)", got, mestekReadRetries+1)
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}

func TestMESTEKGateWaitsForContextAndSerializesCallers(t *testing.T) {
	svc := NewService(DefaultConfig(), nil)
	if err := svc.acquireMESTEK(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := svc.acquireMESTEK(ctx); err != context.DeadlineExceeded {
		t.Fatalf("second gate acquisition = %v, want deadline", err)
	}
	svc.releaseMESTEK()
	if err := svc.acquireMESTEK(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.releaseMESTEK()
}

func TestMESTEKActiveConfigFromEnv(t *testing.T) {
	t.Setenv("ELECTRICAL_MESTEK_ACTIVE_READ", "false")
	t.Setenv("ELECTRICAL_MESTEK_READ_INTERVAL", "12s")
	t.Setenv("ELECTRICAL_6485_READ_INTERVAL", "4s")
	t.Setenv("INFLUXDB_TOKEN", "test-token")
	t.Setenv("INFLUXDB_ADDR", "http://influx.invalid")
	t.Setenv("INFLUXDB_ORG", "lab")
	t.Setenv("INFLUXDB_BUCKET", "electrical")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MESTEKActiveRead || cfg.MESTEKReadInterval != 12*time.Second || cfg.SingleShotTimeout != 30*time.Second {
		t.Fatalf("unexpected MESTEK config: %+v", cfg)
	}
	if cfg.KeithleyReadInterval != 4*time.Second {
		t.Fatalf("unexpected 6485 interval: %v", cfg.KeithleyReadInterval)
	}
	t.Setenv("ELECTRICAL_MESTEK_READ_INTERVAL", "0s")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("non-positive MESTEK interval was accepted")
	}
	t.Setenv("ELECTRICAL_MESTEK_READ_INTERVAL", "12s")
	t.Setenv("ELECTRICAL_6485_READ_INTERVAL", "0s")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("non-positive 6485 interval was accepted")
	}
}

func TestElectricalDefaultIntervals(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MESTEKReadInterval != 30*time.Second {
		t.Fatalf("MESTEK default interval = %v, want 30s", cfg.MESTEKReadInterval)
	}
	if cfg.KeithleyReadInterval != 3*time.Second {
		t.Fatalf("6485 default interval = %v, want 3s", cfg.KeithleyReadInterval)
	}
}
