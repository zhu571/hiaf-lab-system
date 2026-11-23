package electrical

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	mestekReadWindow  = 300 * time.Millisecond
	mestekRetryDelay  = 120 * time.Millisecond
	mestekReadRetries = 3
)

type mestekModule struct {
	unit     byte
	channels int
	station  uint16
}

var mestekModules = []mestekModule{
	{unit: 0x14, channels: 3, station: 0},
	{unit: 0x15, channels: 10, station: 21},
}

// MESTEKSnapshotError reports incomplete coverage without replacing missing
// channels with delayed, passive or previous-snapshot frames.
type MESTEKSnapshotError struct {
	Mapped  int
	Missing []string
	Causes  []string
}

func (e *MESTEKSnapshotError) Error() string {
	message := fmt.Sprintf("MESTEK active snapshot mapped %d/13 channels", e.Mapped)
	if len(e.Missing) > 0 {
		message += "; missing " + strings.Join(e.Missing, ",")
	}
	if len(e.Causes) > 0 {
		message += "; " + strings.Join(e.Causes, "; ")
	}
	return message
}

// collectActiveMESTEK serializes continuous and manually triggered snapshots.
// FC03 is the only operation allowed beyond this gate; no register-write
// function is implemented or exposed by the electrical monitor.
func (s *Service) collectActiveMESTEK(ctx context.Context) ([]Sample, error) {
	if err := s.acquireMESTEK(ctx); err != nil {
		return nil, err
	}
	defer s.releaseMESTEK()
	samples, stats, err := s.readMESTEKSnapshot(ctx)
	s.updateActiveModbusHealth(stats)
	if err != nil {
		s.invalidateMissingMESTEKChannels(samples)
	}
	recorded := s.record(samples)
	if err != nil {
		s.noteSourceError(SourceMESTEK, err, stats.ResponseFrames == 0)
	}
	return recorded, err
}

func (s *Service) invalidateMissingMESTEKChannels(samples []Sample) {
	present := make(map[string]bool)
	for _, sample := range samples {
		if sample.Source == SourceMESTEK && sample.Channel != "UNMAPPED" {
			present[sample.Channel] = true
		}
	}
	s.mu.Lock()
	for key, sample := range s.latest {
		if sample.Source == SourceMESTEK && sample.Channel != "UNMAPPED" && !present[sample.Channel] {
			delete(s.latest, key)
		}
	}
	s.mu.Unlock()
}

func (s *Service) acquireMESTEK(ctx context.Context) error {
	select {
	case s.mestekGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseMESTEK() {
	<-s.mestekGate
}

func (s *Service) runActiveMESTEK(ctx context.Context) {
	for ctx.Err() == nil {
		_, _ = s.collectActiveMESTEK(ctx)
		if !sleepContext(ctx, s.cfg.MESTEKReadInterval) {
			return
		}
	}
}

func (s *Service) readMESTEKSnapshot(ctx context.Context) ([]Sample, ModbusStats, error) {
	var samples []Sample
	var stats ModbusStats
	var missing []string
	var causes []string
	mapped := 0
	for _, module := range mestekModules {
		head, headStats, err := s.readMESTEKHolding(ctx, module.unit, 0, 5)
		addActiveModbusStats(&stats, headStats)
		if err != nil {
			causes = append(causes, fmt.Sprintf("unit 0x%02x header: %v", module.unit, err))
			for local := 1; local <= module.channels; local++ {
				channel, _ := mestekChannel(module.unit, uint16(local*100))
				missing = append(missing, channel)
			}
			if ctx.Err() != nil {
				return samples, stats, &MESTEKSnapshotError{Mapped: mapped, Missing: missing, Causes: causes}
			}
			continue
		}
		if len(head.Registers) != 5 || head.Registers[1] != uint16(module.channels) || head.Registers[3] != module.station {
			causes = append(causes, fmt.Sprintf("unit 0x%02x header mismatch: got %v", module.unit, head.Registers))
			for local := 1; local <= module.channels; local++ {
				channel, _ := mestekChannel(module.unit, uint16(local*100))
				missing = append(missing, channel)
			}
			continue
		}
		for local := 1; local <= module.channels; local++ {
			start := uint16(local * 100)
			frame, frameStats, readErr := s.readMESTEKHolding(ctx, module.unit, start, 16)
			addActiveModbusStats(&stats, frameStats)
			channel, _ := mestekChannel(module.unit, start)
			if readErr != nil {
				missing = append(missing, channel)
				if len(causes) < 5 {
					causes = append(causes, fmt.Sprintf("%s: %v", channel, readErr))
				}
				if ctx.Err() != nil {
					return samples, stats, &MESTEKSnapshotError{Mapped: mapped, Missing: missing, Causes: causes}
				}
				continue
			}
			mapped++
			samples = append(samples, frame.Samples()...)
		}
	}
	if mapped != 13 {
		return samples, stats, &MESTEKSnapshotError{Mapped: mapped, Missing: missing, Causes: causes}
	}
	return samples, stats, nil
}

func addActiveModbusStats(target *ModbusStats, addition ModbusStats) {
	target.CRCErrors += addition.CRCErrors
	target.DiscardedBytes += addition.DiscardedBytes
	target.PairingErrors += addition.PairingErrors
	target.RequestFrames += addition.RequestFrames
	target.ResponseFrames += addition.ResponseFrames
	target.MappedFrames += addition.MappedFrames
	if addition.MaxGap > target.MaxGap {
		target.MaxGap = addition.MaxGap
	}
}

func (s *Service) updateActiveModbusHealth(stats ModbusStats) {
	s.mu.Lock()
	health := s.health[SourceMESTEK]
	health.CRCErrors += stats.CRCErrors
	health.RequestFrames += stats.RequestFrames
	health.ResponseFrames += stats.ResponseFrames
	health.MappedFrames += stats.MappedFrames
	health.PartialBytes = 0 // every transaction closes its connection and buffer
	if stats.MappedFrames > 0 {
		health.Mapping = MappingPaired
	} else if stats.ResponseFrames > 0 {
		health.Mapping = MappingNone
	}
	s.health[SourceMESTEK] = health
	s.mu.Unlock()
}

func validMESTEKRead(unit byte, start, count uint16) bool {
	if start == 0 && count == 5 {
		return unit == 0x14 || unit == 0x15
	}
	if count != 16 {
		return false
	}
	_, ok := mestekChannel(unit, start)
	return ok
}

func mestekReadRequest(unit byte, start, count uint16) ([]byte, error) {
	if !validMESTEKRead(unit, start, count) {
		return nil, fmt.Errorf("MESTEK read outside fixed FC03 map: unit=0x%02x start=%d count=%d", unit, start, count)
	}
	request := make([]byte, 8)
	request[0], request[1] = unit, 0x03
	binary.BigEndian.PutUint16(request[2:4], start)
	binary.BigEndian.PutUint16(request[4:6], count)
	binary.LittleEndian.PutUint16(request[6:8], CRC16(request[:6]))
	return request, nil
}

func (s *Service) readMESTEKHolding(ctx context.Context, unit byte, start, count uint16) (ModbusFrame, ModbusStats, error) {
	request, err := mestekReadRequest(unit, start, count)
	var stats ModbusStats
	if err != nil {
		return ModbusFrame{}, stats, err
	}
	var lastErr error
	for attempt := 0; attempt < mestekReadRetries; attempt++ {
		if attempt > 0 && !sleepContext(ctx, mestekRetryDelay) {
			return ModbusFrame{}, stats, ctx.Err()
		}
		if ctx.Err() != nil {
			return ModbusFrame{}, stats, ctx.Err()
		}
		frame, attemptStats, readErr := s.readMESTEKAttempt(ctx, request, unit, start, count)
		addActiveModbusStats(&stats, attemptStats)
		if readErr == nil {
			return frame, stats, nil
		}
		lastErr = readErr
	}
	return ModbusFrame{}, stats, fmt.Errorf("FC03 unit=0x%02x start=%d count=%d failed after %d attempts: %w", unit, start, count, mestekReadRetries, lastErr)
}

// readMESTEKAttempt deliberately creates and closes one TCP connection for one
// request. Only a same-unit, same-byte-count, valid-CRC response arriving within
// 300 ms can inherit the requested channel identity.
func (s *Service) readMESTEKAttempt(ctx context.Context, request []byte, unit byte, start, count uint16) (ModbusFrame, ModbusStats, error) {
	var stats ModbusStats
	conn, err := s.dial(ctx, "tcp", s.cfg.MESTEKAddr)
	if err != nil {
		return ModbusFrame{}, stats, err
	}
	defer conn.Close()
	n, err := conn.Write(request)
	if err != nil {
		return ModbusFrame{}, stats, err
	}
	if n != len(request) {
		return ModbusFrame{}, stats, io.ErrShortWrite
	}
	stats.RequestFrames++
	deadline := time.Now().Add(mestekReadWindow)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return ModbusFrame{}, stats, err
	}

	buffer := make([]byte, 0, 1024)
	chunk := make([]byte, 4096)
	var readErr error
	for {
		for len(buffer) >= 3 {
			if buffer[1] != 0x03 || (buffer[0] != 0x14 && buffer[0] != 0x15) {
				buffer = buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			byteCount := buffer[2]
			if byteCount < 2 || byteCount > 250 || byteCount%2 != 0 {
				buffer = buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			frameLength := 5 + int(byteCount)
			if len(buffer) < frameLength {
				break
			}
			candidate := buffer[:frameLength]
			if !ValidCRC(candidate) {
				stats.CRCErrors++
				buffer = buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			raw := append([]byte(nil), candidate...)
			buffer = buffer[frameLength:]
			stats.ResponseFrames++
			if raw[0] != unit || raw[2] != byte(2*count) {
				continue
			}
			frame := ModbusFrame{
				Time: s.now(), Kind: "response", Unit: unit, Function: 0x03,
				Start: start, Count: count, ByteCount: raw[2], Raw: raw,
				Registers: make([]uint16, count), Mapping: MappingPaired,
			}
			frame.Channel, _ = mestekChannel(unit, start)
			for index := range frame.Registers {
				frame.Registers[index] = binary.BigEndian.Uint16(raw[3+2*index : 5+2*index])
			}
			frame.Signature = responseSignature(frame.Unit, frame.Function, frame.Registers)
			if frame.Channel != "" { // table headers are validated but are not channels
				stats.MappedFrames++
			}
			return frame, stats, nil
		}
		if readErr != nil {
			return ModbusFrame{}, stats, readErr
		}
		if ctx.Err() != nil {
			return ModbusFrame{}, stats, ctx.Err()
		}
		if len(buffer) > maxModbusBuffer {
			drop := len(buffer) - 256
			stats.DiscardedBytes += uint64(drop)
			buffer = append([]byte(nil), buffer[drop:]...)
		}
		var n int
		n, readErr = conn.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
		}
	}
}
