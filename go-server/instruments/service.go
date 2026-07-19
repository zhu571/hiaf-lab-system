package instruments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// epicsGateway is the base URL for the EPICS gateway service.
const epicsGateway = "http://localhost:5070"

// Service wraps calls to the EPICS gateway.
type Service struct {
	client *http.Client
}

// NewService creates an instruments Service with a 10s timeout HTTP client.
func NewService() *Service {
	return &Service{client: &http.Client{Timeout: 10 * time.Second}}
}

// gatewayPV represents a single PV value from the EPICS gateway.
type gatewayPV struct {
	PV    string          `json:"pv"`
	Value json.RawMessage `json:"value"`
}

func (s *Service) getRawPV(name string) (json.RawMessage, error) {
	resp, err := s.client.Get(epicsGateway + "/" + name)
	if err != nil {
		return nil, fmt.Errorf("epics gateway get %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("epics gateway returned %d for %s: %s", resp.StatusCode, name, string(body))
	}
	var pv gatewayPV
	if err := json.NewDecoder(resp.Body).Decode(&pv); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", name, err)
	}
	if len(pv.Value) == 0 || string(pv.Value) == "null" {
		return nil, fmt.Errorf("%s value is null", name)
	}
	return pv.Value, nil
}

// getPV fetches a float64 PV from the gateway by name.
func (s *Service) getPV(name string) (float64, error) {
	raw, err := s.getRawPV(name)
	if err != nil {
		return 0, err
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.Float64()
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return 0, fmt.Errorf("decode %s numeric value: %w", name, err)
	}
	v, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, fmt.Errorf("decode %s numeric string: %w", name, err)
	}
	return v, nil
}

// getStringPV fetches a string PV from the gateway.
func (s *Service) getStringPV(name string) (string, error) {
	raw, err := s.getRawPV(name)
	if err != nil {
		return "", err
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}
	return "", fmt.Errorf("decode %s string value", name)
}

// getIntPV fetches an int PV from the gateway.
func (s *Service) getIntPV(name string) (int, error) {
	v, err := s.getPV(name)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

func (s *Service) optionalPV(name string) float64 {
	v, _ := s.getPV(name)
	return v
}

func (s *Service) optionalIntPV(name string) int {
	v, _ := s.getIntPV(name)
	return v
}

func (s *Service) optionalStringPV(name string) string {
	v, _ := s.getStringPV(name)
	return v
}

// putPV sends a POST to the gateway to set a PV value.
func (s *Service) putPV(name string, value any) error {
	body, err := json.Marshal(map[string]any{"value": value})
	if err != nil {
		return fmt.Errorf("marshal setpoint: %w", err)
	}
	resp, err := s.client.Post(epicsGateway+"/"+name, "application/json", bytes.NewReader(body))
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
		A1:         a1,
		ValveSP:    valveSP,
		Running:    runningInt != 0,
		Error:      errMsg,
		Setpoint:   s.optionalPV("GasCell:Piezo:Setpoint"),
		Cycle:      s.optionalIntPV("GasCell:Piezo:Cycle"),
		A5Trip:     s.optionalIntPV("GasCell:Safety:A5Trip"),
		A5TripPV:   s.optionalStringPV("GasCell:Safety:A5TripPV"),
		A5TripTime: s.optionalStringPV("GasCell:Safety:A5TripTime"),
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

// PiezoParams reads Kp, Ki, and A5Max from the gateway.
func (s *Service) PiezoParams() (*ParamsResponse, error) {
	kp, err := s.getPV("GasCell:Piezo:Kp")
	if err != nil {
		return nil, err
	}
	ki, err := s.getPV("GasCell:Piezo:Ki")
	if err != nil {
		return nil, err
	}
	a5Max, err := s.getPV("GasCell:Safety:A5Max")
	if err != nil {
		return nil, err
	}
	return &ParamsResponse{Kp: kp, Ki: ki, A5Max: a5Max}, nil
}

// PiezoSetParams writes Kp and Ki to the gateway.
func (s *Service) PiezoSetParams(kp, ki float64) error {
	if err := s.putPV("GasCell:Piezo:Kp", kp); err != nil {
		return err
	}
	return s.putPV("GasCell:Piezo:Ki", ki)
}

// PiezoSetValve sets the valve position (0-100%).
func (s *Service) PiezoSetValve(value float64) error {
	return s.putPV("GasCell:Piezo:ValveSP", value)
}

// PiezoSetSafety writes A5Max (if non-nil) and/or triggers A5Clear.
func (s *Service) PiezoSetSafety(a5Max *float64, a5Clear bool) error {
	if a5Max != nil {
		if err := s.putPV("GasCell:Safety:A5Max", *a5Max); err != nil {
			return err
		}
	}
	if a5Clear {
		return s.putPV("GasCell:Safety:A5Clear", 1)
	}
	return nil
}
