package instruments

// PiezoStatus is the full piezo instrument state returned by the status endpoint.
type PiezoStatus struct {
	A1         float64 `json:"a1"`
	ValveSP    float64 `json:"valve_sp"`
	Running    bool    `json:"running"`
	Error      string  `json:"error,omitempty"`
	Setpoint   float64 `json:"setpoint"`
	Cycle      int     `json:"cycle"`
	A5Trip     int     `json:"a5_trip"`
	A5TripPV   string  `json:"a5_trip_pv,omitempty"`
	A5TripTime string  `json:"a5_trip_time,omitempty"`
}

// SetpointRequest is the body for POST /piezo/setpoint.
type SetpointRequest struct {
	Value float64 `json:"value"`
}

// ValveRequest is the body for POST /piezo/valve.
type ValveRequest struct {
	Value float64 `json:"value"`
}

// ParamsWriteRequest is the body for POST /piezo/params (write Kp/Ki).
type ParamsWriteRequest struct {
	Kp float64 `json:"kp"`
	Ki float64 `json:"ki"`
}

// ParamsResponse is the response for GET /piezo/params.
type ParamsResponse struct {
	Kp    float64 `json:"kp"`
	Ki    float64 `json:"ki"`
	A5Max float64 `json:"a5_max"`
}

// SafetyRequest is the body for POST /piezo/safety.
type SafetyRequest struct {
	A5Max   *float64 `json:"a5_max,omitempty"`
	A5Clear bool     `json:"a5_clear"`
}
