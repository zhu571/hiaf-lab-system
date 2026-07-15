package instruments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Service wraps calls to the EPICS gateway.
type Service struct {
	client  *http.Client
	gateway string
}

// NewService creates an instruments Service with a 10s timeout HTTP client.
func NewService() (*Service, error) {
	gateway := os.Getenv("EPICS_GATEWAY_ADDR")
	if gateway == "" {
		return nil, fmt.Errorf("EPICS_GATEWAY_ADDR is required")
	}
	return NewServiceWithGateway(gateway), nil
}

// NewServiceWithGateway creates a Service for tests and explicit callers.
func NewServiceWithGateway(gateway string) *Service {
	return &Service{
		client:  &http.Client{Timeout: 10 * time.Second},
		gateway: normalizeHTTPBase(gateway),
	}
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
	a1, err := s.getPV("GasCell:Piezo:A1")
	if err != nil {
		return nil, err
	}
	valveSP, err := s.getPV("GasCell:Piezo:ValveSP")
	if err != nil {
		return nil, err
	}
	runningInt, err := s.getIntPV("GasCell:Piezo:Running")
	if err != nil {
		return nil, err
	}
	errMsg, _ := s.getStringPV("GasCell:Piezo:Error") // ponytail: Error PV may not exist; ignore fetch failure
	return &PiezoStatus{
		A1:      a1,
		ValveSP: valveSP,
		Running: runningInt != 0,
		Error:   errMsg,
	}, nil
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
	return s.putPV("GasCell:Piezo:Setpoint", value)
}
