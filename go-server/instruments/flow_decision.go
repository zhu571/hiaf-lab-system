package instruments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (s *Service) NextFlowDecision(ctx context.Context, flow *FlowSession, points []Point, previous any) (*FlowDecision, error) {
	if s.interpretURL == "" || s.interpretToken == "" {
		return nil, fmt.Errorf("py-agent interpreter is not configured")
	}
	commands := []CommandDef{}
	for _, name := range flow.Limits.AllowedCommands {
		def, err := GetCommand(flow.InstrumentID, name)
		if err != nil || def.Risk == "red" {
			return nil, fmt.Errorf("flow contains forbidden command")
		}
		commands = append(commands, *def)
	}
	payload, err := json.Marshal(map[string]any{"trusted_context": map[string]any{"session_id": flow.ID, "instrument_id": flow.InstrumentID, "flow_kind": flow.FlowKind, "objective": flow.Objective, "allowed_commands": commands, "limits": flow.Limits, "frequency_grid": flow.FrequencyGrid, "points": points, "remaining_commands": flow.Limits.MaxCommands - flow.StepCount, "whitelist_version": whitelistVersion}, "untrusted_inputs": map[string]any{"previous_step": previous}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.interpretURL+"/v1/instrument-flow-next", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.interpretToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("py-agent returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	var decision FlowDecision
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&decision); err != nil {
		return nil, fmt.Errorf("decode flow decision: %w", err)
	}
	if decision.Decision != "next_command" && decision.Decision != "complete" && decision.Decision != "abort" {
		return nil, fmt.Errorf("invalid flow decision")
	}
	if decision.Decision == "next_command" {
		allowed := false
		for _, name := range flow.Limits.AllowedCommands {
			if decision.Command == name {
				allowed = true
			}
		}
		if !allowed {
			return nil, fmt.Errorf("flow decision command is forbidden")
		}
		if decision.Reason == "" {
			return nil, fmt.Errorf("flow decision reason is required")
		}
	} else if decision.Command != "" || decision.Params != nil || decision.Reason == "" {
		return nil, fmt.Errorf("terminal flow decision contains command data or lacks reason")
	}
	decision.InputHash, _, _ = canonicalHash(json.RawMessage(payload))
	decision.OutputHash, _, _ = canonicalHash(json.RawMessage(body))
	return &decision, nil
}
