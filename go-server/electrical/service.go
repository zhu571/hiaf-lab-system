package electrical

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrSingleShotRunning = errors.New("another electrical single-shot is running")
	ErrNoSamples         = errors.New("single-shot returned no samples")
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return "electrical single-shot rate limit exceeded"
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type Service struct {
	cfg          Config
	influx       *InfluxClient
	devices      map[string]*SCPIDevice
	dial         dialContextFunc
	now          func() time.Time
	mestekGate   chan struct{}
	keithleyGate chan struct{}

	mu            sync.RWMutex
	latest        map[string]Sample
	health        map[string]SourceHealth
	waiters       map[string][]chan []Sample
	updatedAt     time.Time
	seq           uint64
	running       bool
	runCtx        context.Context
	mestekMisses  int
	mestekBackoff bool

	backlogMu sync.Mutex
	backlog   []Sample

	rateMu        sync.Mutex
	lastSingle    map[string]time.Time
	singleRunning atomic.Bool
	runOnce       sync.Once
	wg            sync.WaitGroup
}

func NewServiceFromEnv() (*Service, error) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewService(cfg, NewInfluxClient(cfg.Influx)), nil
}

func NewService(cfg Config, influx *InfluxClient) *Service {
	if cfg.QueryInterval <= 0 {
		cfg.QueryInterval = 2 * time.Second
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 2 * time.Second
	}
	if cfg.MESTEKReadInterval <= 0 {
		cfg.MESTEKReadInterval = 30 * time.Second
	}
	if cfg.KeithleyReadInterval <= 0 {
		cfg.KeithleyReadInterval = 3 * time.Second
	}
	if cfg.SingleShotTimeout <= 0 {
		cfg.SingleShotTimeout = 30 * time.Second
	}
	if cfg.SingleShotMinInterval <= 0 {
		cfg.SingleShotMinInterval = 5 * time.Second
	}
	if cfg.BacklogCapacity <= 0 {
		cfg.BacklogCapacity = 4096
	}
	if cfg.WriteBatchSize <= 0 {
		cfg.WriteBatchSize = 500
	}
	s := &Service{
		cfg: cfg, influx: influx, devices: make(map[string]*SCPIDevice),
		dial: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, now: time.Now,
		mestekGate:   make(chan struct{}, 1),
		keithleyGate: make(chan struct{}, 1),
		latest:       make(map[string]Sample), health: make(map[string]SourceHealth),
		waiters: make(map[string][]chan []Sample), lastSingle: make(map[string]time.Time),
		backlog: make([]Sample, 0, cfg.BacklogCapacity),
	}
	for _, source := range sourceOrder {
		mode := "scpi"
		if source == SourceMESTEK {
			mode = "modbus_active"
			if !cfg.MESTEKActiveRead {
				mode = "passive"
			}
		}
		status := "connecting"
		if !cfg.Enabled || (source == SourceMESTEK && !cfg.MESTEKEnabled) {
			status = "disabled"
		}
		s.health[source] = SourceHealth{Source: source, Mode: mode, Status: status}
	}
	for _, device := range cfg.Devices {
		s.devices[device.Source] = NewSCPIDevice(device)
	}
	return s
}

// Run owns all background workers. Continuous acquisition is opt-in in code,
// while the bounded Influx backlog also serves single-shot requests when disabled.
func (s *Service) Run(ctx context.Context) {
	s.runOnce.Do(func() {
		s.mu.Lock()
		s.runCtx = ctx
		s.running = s.cfg.Enabled
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.flushLoop(ctx)
		}()
		if s.cfg.Enabled {
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.runKeithley(ctx) }()
			if s.cfg.MESTEKEnabled {
				s.wg.Add(1)
				go func() { defer s.wg.Done(); s.runMESTEK(ctx) }()
			}
			for _, device := range s.devices {
				s.wg.Add(1)
				go func() { defer s.wg.Done(); s.runSCPI(ctx, device) }()
			}
		}
	})
	s.wg.Wait()
}

func (s *Service) continuousRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running && s.runCtx != nil && s.runCtx.Err() == nil
}

func (s *Service) runSCPI(ctx context.Context, device *SCPIDevice) {
	ticker := time.NewTicker(s.cfg.QueryInterval)
	defer ticker.Stop()
	for {
		_, _ = s.collectSCPI(ctx, device)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) collectSCPI(ctx context.Context, device *SCPIDevice) ([]Sample, error) {
	source := device.cfg.Source
	samples, err := device.Collect(ctx)
	recorded := s.record(samples)
	if err != nil {
		s.noteSourceError(source, err, len(samples) == 0)
	} else {
		s.noteSourceSuccess(source)
	}
	return recorded, err
}

func (s *Service) runMESTEK(ctx context.Context) {
	if s.cfg.MESTEKActiveRead {
		s.runActiveMESTEK(ctx)
		return
	}
	parser := NewModbusParser()
	for ctx.Err() == nil {
		conn, err := s.dial(ctx, "tcp", s.cfg.MESTEKAddr)
		if err != nil {
			s.noteSourceError(SourceMESTEK, err, true)
			if !sleepContext(ctx, s.cfg.ReconnectDelay) {
				return
			}
			continue
		}
		s.noteConnected(SourceMESTEK)
		for ctx.Err() == nil {
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			buffer := make([]byte, 8192)
			n, readErr := conn.Read(buffer) // passive boundary: never conn.Write
			if n > 0 {
				frames := parser.Feed(buffer[:n], s.now())
				s.updateModbusHealth(parser)
				for _, frame := range frames {
					if frame.Kind == "response" {
						s.record(frame.Samples())
					}
				}
			}
			if readErr != nil {
				if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() && ctx.Err() == nil {
					continue
				}
				s.noteSourceError(SourceMESTEK, readErr, true)
				break
			}
		}
		_ = conn.Close()
		parser.buffer = nil
		parser.Stats.PartialBytes = 0
		if !sleepContext(ctx, s.cfg.ReconnectDelay) {
			return
		}
	}
}

// runKeithley actively polls the 6485 instead of waiting for an unsolicited
// stream. The device only answers :READ?, so a passive socket never produced
// samples; LabVIEW does not share this endpoint, so polling has no contention.
func (s *Service) runKeithley(ctx context.Context) {
	parser := &KeithleyParser{}
	for ctx.Err() == nil {
		if _, err := s.collectActiveKeithley(ctx, parser); err != nil {
			s.noteSourceError(Source6485, err, true)
		} else {
			s.noteSourceSuccess(Source6485)
		}
		if !sleepContext(ctx, s.cfg.KeithleyReadInterval) {
			return
		}
	}
}

func (s *Service) acquireKeithley(ctx context.Context) error {
	select {
	case s.keithleyGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseKeithley() {
	<-s.keithleyGate
}

// collectActiveKeithley performs one :READ? poll while holding a gate so the
// continuous worker and single-shot never issue concurrent requests.
func (s *Service) collectActiveKeithley(ctx context.Context, parser *KeithleyParser) ([]Sample, error) {
	if err := s.acquireKeithley(ctx); err != nil {
		return nil, err
	}
	defer s.releaseKeithley()
	line, err := s.queryKeithley(ctx)
	if err != nil {
		return nil, err
	}
	samples := parser.Feed([]byte(line+"\n"), s.now())
	s.updateKeithleyHealth(parser)
	if len(samples) == 0 {
		return nil, fmt.Errorf("unrecognized Keithley 6485 response %q", line)
	}
	return s.record(samples), nil
}

const (
	keithleyReadWindow = 2 * time.Second
	// The 6485 terminates responses with CR (0x0D), not LF, and after previous
	// failed polls its TCP buffer can hold several readings at once. Both windows
	// are intentionally short: one drains stale backlog before a new request, the
	// other collects the remaining readings of a burst once the first valid line
	// arrives, so the newest reading wins without always waiting the full window.
	keithleyDrainWindow  = 50 * time.Millisecond
	keithleySettleWindow = 100 * time.Millisecond
	keithleyArmCommand   = ":ARM:COUNT 1"
	keithleyReadCommand  = ":READ?"
)

// queryKeithley arms one reading and triggers it. Only these two read-path
// commands may ever be written; output, ranging and other device state stay
// untouched.
func (s *Service) queryKeithley(ctx context.Context) (string, error) {
	conn, err := s.dial(ctx, "tcp", s.cfg.KeithleyAddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(keithleyReadWindow)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	s.drainKeithley(conn, deadline)
	if err := conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	for _, command := range []string{keithleyArmCommand, keithleyReadCommand} {
		if !allowedKeithleyCommand(command) {
			return "", fmt.Errorf("Keithley command is not in the read-only whitelist")
		}
		if _, err := conn.Write([]byte(command + "\n")); err != nil {
			return "", err
		}
	}
	return readKeithleyReading(conn, deadline)
}

// drainKeithley discards bytes left over from earlier failed polls. A stale
// multi-reading backlog must never be attributed to the request being issued.
func (s *Service) drainKeithley(conn net.Conn, overall time.Time) {
	drainDeadline := time.Now().Add(keithleyDrainWindow)
	if overall.Before(drainDeadline) {
		drainDeadline = overall
	}
	if err := conn.SetReadDeadline(drainDeadline); err != nil {
		return
	}
	buffer := make([]byte, 4096)
	for {
		if !time.Now().Before(drainDeadline) {
			return
		}
		n, err := conn.Read(buffer)
		if n == 0 || err != nil {
			return
		}
	}
}

// readKeithleyReading splits on both CR and LF, validates every candidate with
// the 6485 line pattern and returns the last valid reading of the burst. Echos,
// partial lines and garbage are ignored.
func readKeithleyReading(conn net.Conn, overall time.Time) (string, error) {
	buffer := make([]byte, 4096)
	var pending []byte
	var lastValid string
	for {
		n, readErr := conn.Read(buffer)
		if n > 0 {
			pending = append(pending, buffer[:n]...)
			start := 0
			for index := 0; index < len(pending); index++ {
				if pending[index] != '\r' && pending[index] != '\n' {
					continue
				}
				line := strings.TrimSpace(string(pending[start:index]))
				start = index + 1
				if line != "" && keithleyLinePattern.MatchString(line) {
					lastValid = line
				}
			}
			if start > 0 {
				pending = append([]byte(nil), pending[start:]...)
			}
			if lastValid != "" {
				// More readings may already be queued on this socket; keep
				// reading briefly so the newest one is the one returned.
				idleDeadline := time.Now().Add(keithleySettleWindow)
				if overall.Before(idleDeadline) {
					idleDeadline = overall
				}
				_ = conn.SetReadDeadline(idleDeadline)
			}
		}
		if !time.Now().Before(overall) {
			if lastValid != "" {
				return lastValid, nil
			}
			return "", fmt.Errorf("no valid Keithley 6485 reading within %s", keithleyReadWindow)
		}
		if readErr != nil {
			if lastValid != "" {
				return lastValid, nil
			}
			if len(pending) > 0 {
				return "", fmt.Errorf("no valid Keithley 6485 reading in %q: %w", string(pending), readErr)
			}
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				return "", fmt.Errorf("no valid Keithley 6485 reading within %s", keithleyReadWindow)
			}
			return "", readErr
		}
	}
}

func allowedKeithleyCommand(command string) bool {
	return command == keithleyArmCommand || command == keithleyReadCommand
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func sampleKey(sample Sample) string {
	return strings.Join([]string{sample.Source, sample.Channel, sample.Metric, sample.Mapping, sample.Signature}, "\x00")
}

func (s *Service) record(samples []Sample) []Sample {
	if len(samples) == 0 {
		return nil
	}
	recorded := make([]Sample, len(samples))
	copy(recorded, samples)
	notified := make(map[string][]chan []Sample)
	s.mu.Lock()
	for i := range recorded {
		if recorded[i].Time.IsZero() {
			recorded[i].Time = s.now()
		}
		s.seq++
		recorded[i].Seq = s.seq
		s.latest[sampleKey(recorded[i])] = recorded[i]
		health := s.health[recorded[i].Source]
		health.SampleCount++
		at := recorded[i].Time
		health.LastSampleAt = &at
		health.LastError = ""
		health.Connected = true
		health.Status = "ok"
		s.health[recorded[i].Source] = health
	}
	s.updatedAt = s.now()
	for source, waiters := range s.waiters {
		for _, sample := range recorded {
			if sample.Source == source {
				notified[source] = waiters
				delete(s.waiters, source)
				break
			}
		}
	}
	s.mu.Unlock()
	if err := s.enqueue(recorded); err != nil {
		for _, source := range sampleSources(recorded) {
			s.noteStorageError(source, err)
		}
	}
	for source, waiters := range notified {
		var sourceSamples []Sample
		for _, sample := range recorded {
			if sample.Source == source {
				sourceSamples = append(sourceSamples, sample)
			}
		}
		for _, waiter := range waiters {
			select {
			case waiter <- sourceSamples:
			default:
			}
		}
	}
	return recorded
}

func sampleSources(samples []Sample) []string {
	seen := make(map[string]bool)
	var sources []string
	for _, sample := range samples {
		if !seen[sample.Source] {
			seen[sample.Source] = true
			sources = append(sources, sample.Source)
		}
	}
	return sources
}

func (s *Service) noteConnected(source string) {
	s.mu.Lock()
	health := s.health[source]
	health.Connected = true
	health.Status = "ok"
	health.LastError = ""
	s.health[source] = health
	s.mu.Unlock()
}

func (s *Service) noteSourceSuccess(source string) {
	s.noteConnected(source)
}

func (s *Service) noteSourceError(source string, err error, disconnected bool) {
	if err == nil {
		return
	}
	s.mu.Lock()
	health := s.health[source]
	health.LastError = err.Error()
	health.ErrorCount++
	health.Status = "degraded"
	if disconnected {
		health.Connected = false
	}
	s.health[source] = health
	s.mu.Unlock()
}

func (s *Service) noteStorageError(source string, err error) {
	s.mu.Lock()
	health := s.health[source]
	health.StorageError = err.Error()
	health.WriteErrors++
	health.Status = "degraded"
	s.health[source] = health
	s.mu.Unlock()
}

func (s *Service) noteStorageSuccess(source string) {
	s.mu.Lock()
	health := s.health[source]
	health.StorageError = ""
	if health.LastError == "" {
		health.Status = "ok"
	}
	s.health[source] = health
	s.mu.Unlock()
}

func (s *Service) updateModbusHealth(parser *ModbusParser) {
	s.mu.Lock()
	health := s.health[SourceMESTEK]
	health.CRCErrors = parser.Stats.CRCErrors
	health.PartialBytes = parser.Stats.PartialBytes
	health.PairingErrors = parser.Stats.PairingErrors
	health.RequestFrames = parser.Stats.RequestFrames
	health.ResponseFrames = parser.Stats.ResponseFrames
	health.MappedFrames = parser.Stats.MappedFrames
	health.MaxGapMS = parser.Stats.MaxGap.Milliseconds()
	if parser.Stats.RequestFrames > 0 {
		health.Mapping = MappingPaired
	} else if parser.Stats.ResponseFrames > 0 {
		health.Mapping = MappingNone
	}
	s.health[SourceMESTEK] = health
	s.mu.Unlock()
}

func (s *Service) updateKeithleyHealth(parser *KeithleyParser) {
	s.mu.Lock()
	health := s.health[Source6485]
	health.PartialBytes = parser.PartialBytes
	health.MaxGapMS = parser.MaxGap.Milliseconds()
	if parser.ParseErrors > health.ErrorCount {
		health.ErrorCount = parser.ParseErrors
	}
	s.health[Source6485] = health
	s.mu.Unlock()
}

func (s *Service) Latest() LatestResult {
	s.mu.RLock()
	result := LatestResult{
		ContinuousEnabled: s.continuousRunningUnsafe(), UpdatedAt: s.updatedAt,
		Sources: make([]SourceHealth, 0, len(sourceOrder)), Samples: make([]Sample, 0, len(s.latest)),
	}
	for _, source := range sourceOrder {
		health := s.health[source]
		if source == SourceMESTEK && !s.cfg.MESTEKEnabled {
			health.Status = "disabled"
		} else if health.StorageError != "" || health.LastError != "" {
			health.Status = "degraded"
		} else if health.LastSampleAt != nil || health.Connected {
			health.Status = "ok"
		} else if !s.cfg.Enabled {
			health.Status = "disabled"
		} else {
			health.Status = "connecting"
		}
		result.Sources = append(result.Sources, health)
	}
	for _, sample := range s.latest {
		result.Samples = append(result.Samples, sample)
	}
	s.mu.RUnlock()
	if result.UpdatedAt.IsZero() {
		result.UpdatedAt = s.now()
	}
	s.backlogMu.Lock()
	result.WriteBacklog = len(s.backlog)
	s.backlogMu.Unlock()
	order := make(map[string]int, len(sourceOrder))
	for i, source := range sourceOrder {
		order[source] = i
	}
	sort.Slice(result.Samples, func(i, j int) bool {
		a, b := result.Samples[i], result.Samples[j]
		if order[a.Source] != order[b.Source] {
			return order[a.Source] < order[b.Source]
		}
		if a.Channel != b.Channel {
			return a.Channel < b.Channel
		}
		if a.Signature != b.Signature {
			return a.Signature < b.Signature
		}
		return a.Metric < b.Metric
	})
	return result
}

func (s *Service) continuousRunningUnsafe() bool {
	return s.running && s.runCtx != nil && s.runCtx.Err() == nil
}

func (s *Service) History(ctx context.Context, params HistoryParams) ([]Sample, error) {
	return s.influx.History(ctx, params)
}

func (s *Service) enqueue(samples []Sample) error {
	s.backlogMu.Lock()
	defer s.backlogMu.Unlock()
	if len(s.backlog)+len(samples) > s.cfg.BacklogCapacity {
		return fmt.Errorf("influx backlog full (%d samples)", s.cfg.BacklogCapacity)
	}
	s.backlog = append(s.backlog, samples...)
	return nil
}

func (s *Service) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.flushBacklog(ctx)
		}
	}
}

func (s *Service) flushBacklog(ctx context.Context) {
	s.backlogMu.Lock()
	if len(s.backlog) == 0 {
		s.backlogMu.Unlock()
		return
	}
	count := s.cfg.WriteBatchSize
	if count > len(s.backlog) {
		count = len(s.backlog)
	}
	batch := append([]Sample(nil), s.backlog[:count]...)
	s.backlog = append([]Sample(nil), s.backlog[count:]...)
	s.backlogMu.Unlock()
	if s.influx == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := s.influx.WriteBatch(writeCtx, batch)
	cancel()
	if err == nil {
		for _, source := range sampleSources(batch) {
			s.noteStorageSuccess(source)
		}
		return
	}
	s.backlogMu.Lock()
	combined := append(batch, s.backlog...)
	if len(combined) > s.cfg.BacklogCapacity {
		combined = combined[:s.cfg.BacklogCapacity]
	}
	s.backlog = combined
	s.backlogMu.Unlock()
	for _, source := range sampleSources(batch) {
		s.noteStorageError(source, err)
	}
}

type acquisitionResult struct {
	samples []Sample
	err     error
}

func (s *Service) SingleShot(ctx context.Context, userID string) (SingleShotResult, error) {
	if !s.singleRunning.CompareAndSwap(false, true) {
		return SingleShotResult{}, ErrSingleShotRunning
	}
	defer s.singleRunning.Store(false)
	started := s.now()
	s.rateMu.Lock()
	if last := s.lastSingle[userID]; !last.IsZero() && started.Sub(last) < s.cfg.SingleShotMinInterval {
		retry := s.cfg.SingleShotMinInterval - started.Sub(last)
		s.rateMu.Unlock()
		return SingleShotResult{}, &RateLimitError{RetryAfter: retry}
	}
	s.lastSingle[userID] = started
	s.rateMu.Unlock()

	shotCtx, cancel := context.WithTimeout(ctx, s.cfg.SingleShotTimeout)
	defer cancel()
	result := SingleShotResult{StartedAt: started, Errors: make(map[string]string)}
	continuous := s.continuousRunning()
	var mestekCh <-chan acquisitionResult
	if s.cfg.MESTEKEnabled {
		mestekCh = s.mestekShot(shotCtx, continuous)
	}
	keithleyCh := s.keithleyShot(shotCtx)
	type deviceResult struct {
		source  string
		samples []Sample
		err     error
	}
	deviceResults := make(chan deviceResult, len(s.devices))
	for _, device := range s.devices {
		go func() {
			samples, err := s.collectSCPI(shotCtx, device)
			deviceResults <- deviceResult{source: device.cfg.Source, samples: samples, err: err}
		}()
	}
	for range s.devices {
		select {
		case item := <-deviceResults:
			result.Samples = append(result.Samples, item.samples...)
			if item.err != nil {
				result.Errors[item.source] = item.err.Error()
			}
		case <-shotCtx.Done():
			result.Errors["scpi"] = shotCtx.Err().Error()
		}
		if shotCtx.Err() != nil {
			break
		}
	}
	type sourceChannel struct {
		source string
		ch     <-chan acquisitionResult
	}
	passiveResults := make([]sourceChannel, 0, 2)
	if s.cfg.MESTEKEnabled {
		passiveResults = append(passiveResults, sourceChannel{SourceMESTEK, mestekCh})
	}
	passiveResults = append(passiveResults, sourceChannel{Source6485, keithleyCh})
	for _, pending := range passiveResults {
		select {
		case item := <-pending.ch:
			result.Samples = append(result.Samples, item.samples...)
			if item.err != nil {
				result.Errors[pending.source] = item.err.Error()
			} else if len(item.samples) == 0 {
				result.Errors[pending.source] = "no sample received"
			}
		case <-shotCtx.Done():
			result.Errors[pending.source] = shotCtx.Err().Error()
		}
	}
	result.FinishedAt = s.now()
	result.Sources = s.Latest().Sources
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	if len(result.Samples) == 0 {
		if errors.Is(shotCtx.Err(), context.DeadlineExceeded) {
			return result, context.DeadlineExceeded
		}
		return result, ErrNoSamples
	}
	return result, nil
}

func (s *Service) mestekShot(ctx context.Context, continuous bool) <-chan acquisitionResult {
	if !s.cfg.MESTEKActiveRead {
		return s.passiveShot(ctx, SourceMESTEK, continuous)
	}
	ch := make(chan acquisitionResult, 1)
	go func() {
		samples, err := s.collectActiveMESTEK(ctx)
		ch <- acquisitionResult{samples: samples, err: err}
	}()
	return ch
}

func (s *Service) keithleyShot(ctx context.Context) <-chan acquisitionResult {
	ch := make(chan acquisitionResult, 1)
	go func() {
		samples, err := s.collectActiveKeithley(ctx, &KeithleyParser{})
		ch <- acquisitionResult{samples: samples, err: err}
	}()
	return ch
}

func (s *Service) passiveShot(ctx context.Context, source string, continuous bool) <-chan acquisitionResult {
	ch := make(chan acquisitionResult, 1)
	if continuous {
		waiter := s.addWaiter(ctx, source)
		go func() {
			select {
			case samples := <-waiter:
				ch <- acquisitionResult{samples: samples}
			case <-ctx.Done():
				ch <- acquisitionResult{err: ctx.Err()}
			}
		}()
		return ch
	}
	go func() {
		samples, err := s.captureMESTEK(ctx)
		if err != nil {
			s.noteSourceError(source, err, true)
		}
		ch <- acquisitionResult{samples: s.record(samples), err: err}
	}()
	return ch
}

func (s *Service) addWaiter(ctx context.Context, source string) <-chan []Sample {
	ch := make(chan []Sample, 1)
	s.mu.Lock()
	s.waiters[source] = append(s.waiters[source], ch)
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.removeWaiter(source, ch)
	}()
	return ch
}

func (s *Service) removeWaiter(source string, target chan []Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := s.waiters[source]
	for index, waiter := range waiters {
		if waiter == target {
			waiters = append(waiters[:index], waiters[index+1:]...)
			break
		}
	}
	if len(waiters) == 0 {
		delete(s.waiters, source)
	} else {
		s.waiters[source] = waiters
	}
}

func (s *Service) captureMESTEK(ctx context.Context) ([]Sample, error) {
	conn, err := s.dial(ctx, "tcp", s.cfg.MESTEKAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	parser := NewModbusParser()
	for ctx.Err() == nil {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetReadDeadline(deadline)
		}
		buffer := make([]byte, 8192)
		n, readErr := conn.Read(buffer) // passive boundary: never conn.Write
		if n > 0 {
			for _, frame := range parser.Feed(buffer[:n], s.now()) {
				if frame.Kind == "response" {
					s.updateModbusHealth(parser)
					return frame.Samples(), nil
				}
			}
			s.updateModbusHealth(parser)
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return nil, ctx.Err()
}
