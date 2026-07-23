package instruments

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Service wraps calls to the EPICS gateway.
type Service struct {
	client         *http.Client
	gateway        string
	interpretURL   string
	interpretToken string
}

const (
	piezoSetpointMin = 0.0
	piezoSetpointMax = 100.0
)

var (
	ErrGasCellPermission = fmt.Errorf("gascell control permission denied")
	ErrGasCellGateway    = fmt.Errorf("gascell gateway error")
)

// NewService creates an instruments Service with a bounded HTTP client.
func NewService() (*Service, error) {
	gateway := os.Getenv("EPICS_GATEWAY_ADDR")
	if gateway == "" {
		return nil, fmt.Errorf("EPICS_GATEWAY_ADDR is required")
	}
	svc := NewServiceWithGateway(gateway)
	svc.interpretURL = strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	tokenPath := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE")
	if tokenPath != "" {
		token, err := os.ReadFile(filepath.Clean(tokenPath))
		if err != nil {
			return nil, fmt.Errorf("read py-agent internal token: %w", err)
		}
		svc.interpretToken = strings.TrimSpace(string(token))
	}
	return svc, nil
}

// ConfigureInterpreter sets the internal translator endpoint; used by startup and tests.
func (s *Service) ConfigureInterpreter(url, token string) {
	s.interpretURL = strings.TrimRight(url, "/")
	s.interpretToken = token
}

type interpretResponse struct {
	Status        string         `json:"status"`
	Command       string         `json:"command"`
	Params        map[string]any `json:"params"`
	Confidence    float64        `json:"confidence"`
	Explanation   string         `json:"explanation"`
	Question      string         `json:"question"`
	Reason        string         `json:"reason"`
	PromptVersion string         `json:"prompt_version"`
	Model         string         `json:"model"`
}

// Interpret calls the internal no-tool translator, then validates and renders its candidate locally.
func (s *Service) Interpret(ctx context.Context, instrumentID string, req NLCommandRequest) (*NLCommandCandidate, error) {
	if s.interpretURL == "" || s.interpretToken == "" {
		return nil, fmt.Errorf("py-agent interpreter is not configured")
	}
	commands := make([]CommandDef, 0)
	for _, command := range ListCommands(instrumentID) {
		if command.Risk != "red" {
			commands = append(commands, command)
		}
	}
	payload, err := json.Marshal(map[string]any{
		"instrument_id": instrumentID, "instrument_name": InstrumentName(instrumentID),
		"whitelist_commands": commands, "user_input": req.Input, "history": req.History,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.interpretURL+"/v1/interpret", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.interpretToken)
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("py-agent returned %d", resp.StatusCode)
	}
	var translated interpretResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&translated); err != nil {
		return nil, fmt.Errorf("decode py-agent response: %w", err)
	}
	candidate := &NLCommandCandidate{
		Status: translated.Status, Command: translated.Command, Params: translated.Params,
		Confidence: translated.Confidence, Explanation: translated.Explanation,
		Question: translated.Question, Reason: translated.Reason,
		PromptVersion: translated.PromptVersion, Model: translated.Model,
		WhitelistVersion: whitelistVersion, Validation: NLValidation{OK: translated.Status != "ok"},
	}
	if translated.Status != "ok" {
		if translated.Status != "clarify" && translated.Status != "rejected" {
			return nil, fmt.Errorf("py-agent returned invalid status")
		}
		return candidate, nil
	}
	def, err := GetCommand(instrumentID, translated.Command)
	if err != nil || def.Risk == "red" {
		return nil, fmt.Errorf("py-agent returned forbidden command")
	}
	candidate.Risk = def.Risk
	scpi, normalized, err := RenderSCPI(instrumentID, translated.Command, translated.Params)
	if err != nil {
		candidate.Validation = NLValidation{OK: false, Reasons: []string{err.Error()}}
		return candidate, nil
	}
	candidate.Params, candidate.SCPI = normalized, scpi
	candidate.Validation = NLValidation{OK: true}
	return candidate, nil
}

// NewServiceWithGateway creates a Service for tests and explicit callers.
func NewServiceWithGateway(gateway string) *Service {
	return &Service{
		client:  &http.Client{Timeout: 15 * time.Second},
		gateway: normalizeHTTPBase(gateway),
	}
}

// NewSCPIConnection opens a TCP connection to a SCPI instrument.
func NewSCPIConnection(addr, terminator string) (*SCPIConnection, error) {
	if addr == "" {
		return nil, fmt.Errorf("SCPI address is required")
	}
	if terminator == "" {
		return nil, fmt.Errorf("SCPI terminator is required")
	}
	const timeout = 10 * time.Second
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to SCPI instrument %s: %w", addr, err)
	}
	return &SCPIConnection{addr: addr, terminator: terminator, timeout: timeout, conn: conn}, nil
}

// Send writes each newline- or semicolon-delimited command and reads query responses.
func (c *SCPIConnection) Send(cmd string) (string, error) {
	if c == nil || c.conn == nil {
		return "", fmt.Errorf("SCPI connection is closed")
	}

	var responses []string
	for _, line := range strings.FieldsFunc(cmd, func(r rune) bool { return r == ';' || r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
			return "", fmt.Errorf("set SCPI deadline: %w", err)
		}
		message := line + c.terminator
		n, err := io.WriteString(c.conn, message)
		if err != nil {
			return "", fmt.Errorf("send SCPI command %q: %w", line, err)
		}
		if n != len(message) {
			return "", fmt.Errorf("send SCPI command %q: %w", line, io.ErrShortWrite)
		}
		if !strings.HasSuffix(line, "?") {
			continue
		}
		response, err := bufio.NewReader(c.conn).ReadString(c.terminator[len(c.terminator)-1])
		if err != nil {
			return "", fmt.Errorf("read SCPI response for %q: %w", line, err)
		}
		responses = append(responses, strings.TrimSuffix(response, c.terminator))
	}
	return strings.Join(responses, "\n"), nil
}

// Close closes the instrument connection.
func (c *SCPIConnection) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// gatewayPV represents a single PV value from the EPICS gateway.
type gatewayPV struct {
	PV    string  `json:"pv"`
	Value float64 `json:"value"`
}

// gatewayStringPV is for string-valued PVs (Error).
type gatewayStringPV struct {
	PV    string `json:"pv"`
	Value string `json:"value"`
}

// gatewayRunningPV is for integer-valued PVs (Running).
type gatewayRunningPV struct {
	PV    string `json:"pv"`
	Value int    `json:"value"`
}

var gasCellStatusPVs = []string{
	"GasCell:Piezo:A1",
	"GasCell:Piezo:ValveSP",
	"GasCell:Piezo:Running",
	"GasCell:Piezo:Setpoint",
	"GasCell:Piezo:Kp",
	"GasCell:Piezo:Ki",
	"GasCell:Piezo:Kd",
	"GasCell:Piezo:Error",
	"GasCell:Piezo:Delta",
	"GasCell:Piezo:Cycle",
	"GasCell:Safety:A5Max",
	"GasCell:Safety:A5Trip",
	"GasCell:Safety:A5TripPV",
	"GasCell:Safety:A5TripTime",
	"GasCell:Vac:A5",
}

func normalizeHTTPBase(addr string) string {
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

// getPVRaw fetches a raw PV JSON body from the gateway by name.
func (s *Service) getPVRaw(name string) ([]byte, error) {
	resp, err := s.client.Get(s.gateway + "/" + name)
	if err != nil {
		return nil, fmt.Errorf("epics gateway get %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("epics gateway returned %d for %s: %s", resp.StatusCode, name, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", name, err)
	}
	return body, nil
}

// getPV fetches a float64 PV from the gateway by name.
func (s *Service) getPV(name string) (float64, error) {
	body, err := s.getPVRaw(name)
	if err != nil {
		return 0, err
	}
	var pv gatewayPV
	if err := json.Unmarshal(body, &pv); err != nil {
		return 0, fmt.Errorf("decode %s response: %w", name, err)
	}
	return pv.Value, nil
}

// getStringPV fetches a string PV from the gateway.
func (s *Service) getStringPV(name string) (string, error) {
	body, err := s.getPVRaw(name)
	if err != nil {
		return "", err
	}
	var pv gatewayStringPV
	if err := json.Unmarshal(body, &pv); err != nil {
		return "", fmt.Errorf("decode %s response: %w", name, err)
	}
	return pv.Value, nil
}

// getIntPV fetches an int PV from the gateway.
func (s *Service) getIntPV(name string) (int, error) {
	body, err := s.getPVRaw(name)
	if err != nil {
		return 0, err
	}
	var pv gatewayRunningPV
	if err := json.Unmarshal(body, &pv); err != nil {
		return 0, fmt.Errorf("decode %s response: %w", name, err)
	}
	return pv.Value, nil
}

func (s *Service) getPVValue(name string) (any, error) {
	body, err := s.getPVRaw(name)
	if err != nil {
		return nil, err
	}
	var pv struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(body, &pv); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", name, err)
	}
	return pv.Value, nil
}

// GasCellStatus returns a best-effort aggregate; one unavailable PV does not hide the others.
func (s *Service) GasCellStatus() *GasCellSnapshot {
	snapshot := &GasCellSnapshot{Data: make(map[string]PVPoint, len(gasCellStatusPVs))}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range gasCellStatusPVs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := s.getPVValue(name)
			point := PVPoint{Value: value, Quality: "good"}
			if err != nil || value == nil {
				point = PVPoint{Quality: "disconnected"}
			}
			mu.Lock()
			snapshot.Data[name] = point
			mu.Unlock()
		}()
	}
	wg.Wait()
	return snapshot
}

// putPV sends a POST to the gateway to set a PV value.
func (s *Service) putPV(name string, value any) error {
	body, err := json.Marshal(map[string]any{"value": value})
	if err != nil {
		return fmt.Errorf("marshal setpoint: %w", err)
	}
	resp, err := s.client.Post(s.gateway+"/"+name, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("epics gateway post %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("epics gateway post %s returned %d: %s", name, resp.StatusCode, string(respBody))
	}
	return nil
}

// PiezoStatus returns the combined piezo instrument state.
func (s *Service) PiezoStatus() (*PiezoStatus, error) {
	snapshot := s.GasCellStatus()
	a1, ok := pointNumber(snapshot.Data["GasCell:Piezo:A1"])
	if !ok {
		return nil, fmt.Errorf("GasCell:Piezo:A1 unavailable")
	}
	valveSP, ok := pointNumber(snapshot.Data["GasCell:Piezo:ValveSP"])
	if !ok {
		return nil, fmt.Errorf("GasCell:Piezo:ValveSP unavailable")
	}
	running, ok := pointNumber(snapshot.Data["GasCell:Piezo:Running"])
	if !ok {
		return nil, fmt.Errorf("GasCell:Piezo:Running unavailable")
	}
	errMsg, _ := snapshot.Data["GasCell:Piezo:Error"].Value.(string)
	return &PiezoStatus{
		A1:      a1,
		ValveSP: valveSP,
		Running: running != 0,
		Error:   errMsg,
	}, nil
}

func pointNumber(point PVPoint) (float64, bool) {
	if point.Quality != "good" {
		return 0, false
	}
	return numericValue(point.Value)
}

// PiezoStart sets Running=1.
func (s *Service) PiezoStart() error {
	return s.putPV("GasCell:Piezo:Running", 1)
}

// PiezoStop sets Running=0.
func (s *Service) PiezoStop() error {
	return s.putPV("GasCell:Piezo:Running", 0)
}

// PiezoSetpoint sets the setpoint value.
func (s *Service) PiezoSetpoint(value float64) error {
	if !(value >= piezoSetpointMin && value <= piezoSetpointMax) {
		return fmt.Errorf("piezo setpoint must be between %.1f and %.1f", piezoSetpointMin, piezoSetpointMax)
	}
	return s.putPV("GasCell:Piezo:Setpoint", value)
}

// WriteGasCellPV enforces the MVP object permission, PV contract, and post-write readback.
func (s *Service) WriteGasCellPV(role, name string, value any) (*PVWriteResult, error) {
	if role != "maintainer" && role != "admin" {
		return nil, ErrGasCellPermission
	}
	if err := ValidatePVParams(name, value); err != nil {
		return nil, err
	}
	if err := s.putPV(name, value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGasCellGateway, err)
	}
	readbackPV := name
	expected := value
	if name == "GasCell:Safety:A5Clear" {
		readbackPV, expected = "GasCell:Safety:A5Trip", 0
	}
	readback, err := s.getPVValue(readbackPV)
	result := &PVWriteResult{PV: name, Requested: value, Readback: readback}
	if err != nil {
		result.Warning = "写入已发送，但回读失败"
		return result, nil
	}
	if !readbackMatches(readbackPV, expected, readback) {
		result.Warning = fmt.Sprintf("回读不一致：期望 %v，实际 %v", expected, readback)
	}
	return result, nil
}

func readbackMatches(name string, expected, actual any) bool {
	cfg, ok := gasCellPVConfig[name]
	if !ok {
		return false
	}
	want, wantOK := numericValue(expected)
	got, gotOK := numericValue(actual)
	if wantOK && gotOK {
		return math.Abs(want-got) <= cfg.ReadbackTolerance
	}
	return fmt.Sprint(expected) == fmt.Sprint(actual)
}

func (s *Service) GasCellParams(role string, req GasCellParamsRequest) ([]PVWriteResult, error) {
	writes := []struct {
		name  string
		value *float64
	}{{"GasCell:Piezo:Setpoint", req.Setpoint}, {"GasCell:Piezo:Kp", req.Kp}, {"GasCell:Piezo:Ki", req.Ki}}
	for _, write := range writes {
		if write.value != nil {
			if role != "maintainer" && role != "admin" {
				return nil, ErrGasCellPermission
			}
			if err := ValidatePVParams(write.name, *write.value); err != nil {
				return nil, err
			}
		}
	}
	results := make([]PVWriteResult, 0, 3)
	for _, write := range writes {
		if write.value == nil {
			continue
		}
		result, err := s.WriteGasCellPV(role, write.name, *write.value)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("at least one of setpoint, kp, or ki is required")
	}
	return results, nil
}

func (s *Service) GasCellValve(role string, value float64) (*PVWriteResult, error) {
	running, err := s.getPV("GasCell:Piezo:Running")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGasCellGateway, err)
	}
	if running != 0 {
		return nil, fmt.Errorf("manual valve control requires Running=0")
	}
	return s.WriteGasCellPV(role, "GasCell:Piezo:ValveSP", value)
}
