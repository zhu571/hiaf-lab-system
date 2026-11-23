package electrical

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// fc03ReadGuardConn makes the MESTEK network boundary executable: every socket
// write must be one complete, valid, fixed-map FC03 request. Other function
// codes, register writes and arbitrary payloads fail before reaching the device.
type fc03ReadGuardConn struct {
	net.Conn
}

func (c *fc03ReadGuardConn) Write(data []byte) (int, error) {
	if len(data) != 8 || data[1] != 0x03 || !ValidCRC(data) {
		return 0, errors.New("MESTEK guard rejected a non-FC03 or malformed request")
	}
	unit := data[0]
	start := uint16(data[2])<<8 | uint16(data[3])
	count := uint16(data[4])<<8 | uint16(data[5])
	if !validMESTEKRead(unit, start, count) {
		return 0, errors.New("MESTEK guard rejected an address outside the fixed read map")
	}
	return c.Conn.Write(data)
}

// TestLiveMESTEKActiveThirteenChannels is opt-in because FC03 reads still put
// traffic on the shared serial bus. It never writes a register or changes output.
func TestLiveMESTEKActiveThirteenChannels(t *testing.T) {
	if os.Getenv("ELECTRICAL_LIVE_MESTEK_ACTIVE") != "1" {
		t.Skip("set ELECTRICAL_LIVE_MESTEK_ACTIVE=1 for the guarded FC03 live snapshot")
	}
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	svc.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &fc03ReadGuardConn{Conn: conn}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	samples, err := svc.collectActiveMESTEK(ctx)
	values := make(map[string]map[string]float64)
	for _, sample := range samples {
		if sample.Value == nil {
			continue
		}
		if values[sample.Channel] == nil {
			values[sample.Channel] = make(map[string]float64)
		}
		values[sample.Channel][sample.Metric] = *sample.Value
	}
	for channel := 1; channel <= 13; channel++ {
		name := fmt.Sprintf("CH%d", channel)
		if readings := values[name]; readings != nil {
			t.Logf("%s U=%.2f V I=%.3f A SetU=%.2f V SetI(+11)=%.3f A output=%.0f", name, readings["U_V"], readings["I_A"], readings["setU_V"], readings["setI_A"], readings["output_enabled"])
		} else {
			t.Logf("%s no valid channel response", name)
		}
	}
	health := svc.Latest().Sources[0]
	t.Logf("MESTEK active coverage=%d/13 requests=%d responses=%d mapped=%d crc_errors=%d", len(values), health.RequestFrames, health.ResponseFrames, health.MappedFrames, health.CRCErrors)
	if err != nil {
		t.Fatalf("strict live snapshot was incomplete: %v", err)
	}
	if len(values) != 13 {
		t.Fatalf("strict live snapshot coverage = %d/13", len(values))
	}
}
