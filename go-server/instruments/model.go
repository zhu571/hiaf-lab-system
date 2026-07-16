package instruments

// PiezoStatus is the full piezo instrument state returned by the status endpoint.
type PiezoStatus struct {
	A1      float64 `json:"a1"`
	ValveSP float64 `json:"valve_sp"`
	Running bool    `json:"running"`
	Error   string  `json:"error,omitempty"`
}

// SetpointRequest is the body for POST /piezo/setpoint.
type SetpointRequest struct {
	Value float64 `json:"value"`
}
