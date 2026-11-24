package electrical

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	mestekReadWindow  = 300 * time.Millisecond
	mestekRetryDelay  = 120 * time.Millisecond
	mestekReadRetries = 3

	// LabVIEW shares the same serial gateway and its poll bursts occasionally
	// swallow one FC03 reply for the whole retry window. After a complete sweep,
	// failed channels are retried once after a short settle so the burst can
	// drain; this recovers bursty drops that a longer read window cannot, because
	// the reply never arrives at all rather than arriving late.
	mestekRecoveryDelay = 400 * time.Millisecond

	// LabVIEW is the primary MESTEK client. Losing more than three channels in a
	// round is treated as a severe miss; three severe misses in a row skip one
	// interval so the slave client yields the shared serial line.
	mestekBackoffMinMapped   = 10
	mestekBackoffAfterRounds = 3
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
// channels with delayed, passive or previous-snapshot frames. Partial rounds
// are still recorded; only wholly absent channels remain missing.
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
		// Keep the last known value for channels that dropped out of this round
		// so the UI does not flicker; prune only values older than the retention
		// window. The incomplete round itself is still reported via source health.
		s.pruneStaleMESTEKChannels(samples)
	}
	s.noteMESTEKSnapshot(int(stats.MappedFrames))
	recorded := s.record(samples)
	if err != nil {
		s.noteSourceError(SourceMESTEK, err, stats.ResponseFrames == 0)
	}
	return recorded, err
}

// pruneStaleMESTEKChannels drops channels that were missing from this round and
// whose last value is older than the retention window. Channels missing only for
// a round or two keep their previous reading to avoid UI jitter.
func (s *Service) pruneStaleMESTEKChannels(samples []Sample) {
	present := make(map[string]bool)
	for _, sample := range samples {
		if sample.Source == SourceMESTEK && sample.Channel != "UNMAPPED" {
			present[sample.Channel] = true
		}
	}
	cutoff := s.now().Add(-s.mestekStaleAfter())
	s.mu.Lock()
	for key, sample := range s.latest {
		if sample.Source != SourceMESTEK || sample.Channel == "UNMAPPED" {
			continue
		}
		if !present[sample.Channel] && sample.Time.Before(cutoff) {
			delete(s.latest, key)
		}
	}
	s.mu.Unlock()
}

func (s *Service) mestekStaleAfter() time.Duration {
	retention := 3 * s.cfg.MESTEKReadInterval
	if retention < time.Minute {
		retention = time.Minute
	}
	return retention
}

// noteMESTEKSnapshot tracks consecutive severe misses and arms a single-skipped
// round once the threshold is reached. Healthy or mildly partial rounds reset it.
func (s *Service) noteMESTEKSnapshot(mapped int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapped > mestekBackoffMinMapped {
		s.mestekMisses = 0
		return
	}
	s.mestekMisses++
	if s.mestekMisses >= mestekBackoffAfterRounds {
		s.mestekMisses = 0
		s.mestekBackoff = true
	}
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
		interval := s.cfg.MESTEKReadInterval
		s.mu.Lock()
		if s.mestekBackoff {
			s.mestekBackoff = false
			interval *= 2
		}
		s.mu.Unlock()
		if !sleepContext(ctx, interval) {
			return
		}
	}
}

func (s *Service) readMESTEKSnapshot(ctx context.Context) ([]Sample, ModbusStats, error) {
	snapshot := &mestekSnapshot{service: s}
	defer snapshot.close()

	round := &mestekRound{
		snapshot:   snapshot,
		mapped:     make(map[string]bool),
		headerErr:  make(map[byte]string),
		channelErr: make(map[string]string),
	}
	for _, module := range mestekModules {
		round.readModule(ctx, module)
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() == nil && len(round.mapped) != 13 && len(round.pending) > 0 {
		// A dropped reply usually means LabVIEW's poll burst is holding the
		// shared serial line. Give it a moment to drain, then retry only the
		// reads that failed instead of repeating the whole 13-channel sweep.
		if sleepContext(ctx, mestekRecoveryDelay) {
			round.recover(ctx)
		}
	}
	snapshot.stats.PartialBytes = snapshot.partialBytes()
	if len(round.mapped) != 13 {
		return round.samples, snapshot.stats, round.snapshotError()
	}
	return round.samples, snapshot.stats, nil
}

type mestekPendingRead struct {
	unit    byte
	start   uint16
	channel string // empty for a module header read
}

// mestekRound accumulates one snapshot attempt so a failed read can be retried
// after the sweep without losing already-mapped channels.
type mestekRound struct {
	snapshot   *mestekSnapshot
	samples    []Sample
	mapped     map[string]bool
	pending    []mestekPendingRead
	headerErr  map[byte]string
	channelErr map[string]string
}

func (round *mestekRound) readModule(ctx context.Context, module mestekModule) {
	head, err := round.snapshot.readHolding(ctx, module.unit, 0, 5)
	if err != nil {
		round.headerErr[module.unit] = fmt.Sprintf("unit 0x%02x header: %v", module.unit, err)
		round.pending = append(round.pending, mestekPendingRead{unit: module.unit})
		return
	}
	if !module.acceptsHeader(head) {
		round.headerErr[module.unit] = fmt.Sprintf("unit 0x%02x header mismatch: got %v", module.unit, head.Registers)
		round.pending = append(round.pending, mestekPendingRead{unit: module.unit})
		return
	}
	for local := 1; local <= module.channels; local++ {
		round.readChannel(ctx, module, uint16(local*100))
		if ctx.Err() != nil {
			return
		}
	}
}

func (round *mestekRound) readChannel(ctx context.Context, module mestekModule, start uint16) {
	channel, _ := mestekChannel(module.unit, start)
	frame, err := round.snapshot.readHolding(ctx, module.unit, start, 16)
	if err != nil {
		round.channelErr[channel] = fmt.Sprintf("%s: %v", channel, err)
		round.pending = append(round.pending, mestekPendingRead{
			unit: module.unit, start: start, channel: channel,
		})
		return
	}
	delete(round.channelErr, channel)
	round.mapped[channel] = true
	round.samples = append(round.samples, frame.Samples()...)
}

// recover re-reads only the reads that failed the sweep, once, after the settle
// delay. A module header is re-validated first so its channels are never read
// with an unverified table layout.
func (round *mestekRound) recover(ctx context.Context) {
	retriedHeaders := make(map[byte]bool)
	for _, pending := range round.pending {
		if ctx.Err() != nil {
			return
		}
		module, ok := mestekModuleByUnit(pending.unit)
		if !ok {
			continue
		}
		if pending.channel == "" {
			if retriedHeaders[pending.unit] {
				continue
			}
			retriedHeaders[pending.unit] = true
			head, err := round.snapshot.readHolding(ctx, module.unit, 0, 5)
			if err != nil {
				round.headerErr[module.unit] = fmt.Sprintf("unit 0x%02x header: %v (settle retry)", module.unit, err)
				continue
			}
			if !module.acceptsHeader(head) {
				round.headerErr[module.unit] = fmt.Sprintf("unit 0x%02x header mismatch: got %v (settle retry)", module.unit, head.Registers)
				continue
			}
			delete(round.headerErr, module.unit)
			for local := 1; local <= module.channels; local++ {
				start := uint16(local * 100)
				if channel, _ := mestekChannel(module.unit, start); round.mapped[channel] {
					continue
				}
				round.readChannel(ctx, module, start)
			}
			continue
		}
		if round.mapped[pending.channel] {
			continue
		}
		round.readChannel(ctx, module, pending.start)
	}
}

func (round *mestekRound) snapshotError() *MESTEKSnapshotError {
	var missing, causes []string
	for _, module := range mestekModules {
		if cause, bad := round.headerErr[module.unit]; bad {
			causes = append(causes, cause)
			for local := 1; local <= module.channels; local++ {
				channel, _ := mestekChannel(module.unit, uint16(local*100))
				missing = append(missing, channel)
			}
			continue
		}
		for local := 1; local <= module.channels; local++ {
			channel, _ := mestekChannel(module.unit, uint16(local*100))
			if round.mapped[channel] {
				continue
			}
			missing = append(missing, channel)
			if len(causes) < 5 {
				if cause, ok := round.channelErr[channel]; ok {
					causes = append(causes, cause)
				}
			}
		}
	}
	return &MESTEKSnapshotError{Mapped: len(round.mapped), Missing: missing, Causes: causes}
}

func mestekModuleByUnit(unit byte) (mestekModule, bool) {
	for _, module := range mestekModules {
		if module.unit == unit {
			return module, true
		}
	}
	return mestekModule{}, false
}

func (module mestekModule) acceptsHeader(frame ModbusFrame) bool {
	return len(frame.Registers) == 5 &&
		frame.Registers[1] == uint16(module.channels) &&
		frame.Registers[3] == module.station
}

// mestekSnapshot owns one TCP connection for an entire 13-channel round. Keeping
// the gateway connection open (instead of reconnecting per channel) is what makes
// the slave client stop starving the LabVIEW primary reader.
type mestekSnapshot struct {
	service *Service
	session *mestekSession
	stats   ModbusStats
}

func (snapshot *mestekSnapshot) close() {
	if snapshot.session != nil {
		snapshot.session.close()
		snapshot.session = nil
	}
}

func (snapshot *mestekSnapshot) partialBytes() int {
	if snapshot.session == nil {
		return 0
	}
	return len(snapshot.session.buffer)
}

func (snapshot *mestekSnapshot) readHolding(ctx context.Context, unit byte, start, count uint16) (ModbusFrame, error) {
	request, err := mestekReadRequest(unit, start, count)
	if err != nil {
		return ModbusFrame{}, err
	}
	var lastErr error
	for attempt := 0; attempt < mestekReadRetries; attempt++ {
		if attempt > 0 && !sleepContext(ctx, mestekRetryDelay) {
			return ModbusFrame{}, ctx.Err()
		}
		if ctx.Err() != nil {
			return ModbusFrame{}, ctx.Err()
		}
		if err := snapshot.ensureSession(ctx); err != nil {
			lastErr = err
			continue
		}
		frame, stats, readErr := snapshot.session.transact(ctx, request, unit, start, count, snapshot.service.now)
		addActiveModbusStats(&snapshot.stats, stats)
		if readErr == nil {
			return frame, nil
		}
		lastErr = readErr
		if ctx.Err() != nil {
			return ModbusFrame{}, ctx.Err()
		}
		if !isMESTEKTimeout(readErr) {
			// A timed-out request is retried on the same socket; only a broken
			// socket forces a reconnect on the next attempt.
			snapshot.session.close()
			snapshot.session = nil
		}
	}
	return ModbusFrame{}, fmt.Errorf("FC03 unit=0x%02x start=%d count=%d failed after %d attempts: %w", unit, start, count, mestekReadRetries, lastErr)
}

func (snapshot *mestekSnapshot) ensureSession(ctx context.Context) error {
	if snapshot.session != nil {
		return nil
	}
	session, err := snapshot.service.dialMESTEKSession(ctx)
	if err != nil {
		return err
	}
	snapshot.session = session
	return nil
}

func (s *Service) dialMESTEKSession(ctx context.Context) (*mestekSession, error) {
	conn, err := s.dial(ctx, "tcp", s.cfg.MESTEKAddr)
	if err != nil {
		return nil, err
	}
	return &mestekSession{conn: conn}, nil
}

func isMESTEKTimeout(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

type mestekSession struct {
	conn   net.Conn
	buffer []byte
}

func (session *mestekSession) close() {
	if session != nil && session.conn != nil {
		_ = session.conn.Close()
	}
}

// transact issues one FC03 read on the shared connection. Any bytes buffered
// from an earlier timed-out attempt are discarded first so a duplicate late
// reply cannot be attributed to the next channel.
func (session *mestekSession) transact(ctx context.Context, request []byte, unit byte, start, count uint16, now func() time.Time) (ModbusFrame, ModbusStats, error) {
	var stats ModbusStats
	if len(session.buffer) > 0 {
		stats.DiscardedBytes += uint64(len(session.buffer))
		session.buffer = session.buffer[:0]
	}
	n, err := session.conn.Write(request)
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
	if err := session.conn.SetReadDeadline(deadline); err != nil {
		return ModbusFrame{}, stats, err
	}
	frame, err := session.readFrame(ctx, unit, start, count, now, &stats)
	return frame, stats, err
}

// readFrame only accepts a same-unit, same-byte-count, valid-CRC response
// arriving within the read window. Channel identity comes from the request.
func (session *mestekSession) readFrame(ctx context.Context, unit byte, start, count uint16, now func() time.Time, stats *ModbusStats) (ModbusFrame, error) {
	chunk := make([]byte, 4096)
	var readErr error
	for {
		for len(session.buffer) >= 3 {
			if session.buffer[1] != 0x03 || (session.buffer[0] != 0x14 && session.buffer[0] != 0x15) {
				session.buffer = session.buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			byteCount := session.buffer[2]
			if byteCount < 2 || byteCount > 250 || byteCount%2 != 0 {
				session.buffer = session.buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			frameLength := 5 + int(byteCount)
			if len(session.buffer) < frameLength {
				break
			}
			candidate := session.buffer[:frameLength]
			if !ValidCRC(candidate) {
				stats.CRCErrors++
				session.buffer = session.buffer[1:]
				stats.DiscardedBytes++
				continue
			}
			raw := append([]byte(nil), candidate...)
			session.buffer = session.buffer[frameLength:]
			stats.ResponseFrames++
			if raw[0] != unit || raw[2] != byte(2*count) {
				continue
			}
			frame := ModbusFrame{
				Time: now(), Kind: "response", Unit: unit, Function: 0x03,
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
			return frame, nil
		}
		if readErr != nil {
			return ModbusFrame{}, readErr
		}
		if ctx.Err() != nil {
			return ModbusFrame{}, ctx.Err()
		}
		if len(session.buffer) > maxModbusBuffer {
			drop := len(session.buffer) - 256
			stats.DiscardedBytes += uint64(drop)
			session.buffer = append([]byte(nil), session.buffer[drop:]...)
		}
		var n int
		n, readErr = session.conn.Read(chunk)
		if n > 0 {
			session.buffer = append(session.buffer, chunk[:n]...)
		}
	}
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
	health.PartialBytes = stats.PartialBytes
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
