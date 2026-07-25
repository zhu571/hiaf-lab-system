package instruments

import "testing"

func TestPVConfig(t *testing.T) {
	if len(gasCellPVConfig) != 41 {
		t.Fatalf("expected 41 GasCell PVs, got %d", len(gasCellPVConfig))
	}
	for _, test := range []struct {
		name  string
		value any
		ok    bool
	}{
		{"GasCell:Piezo:Setpoint", 12.5, true},
		{"GasCell:Piezo:Running", 1, true},
		{"GasCell:Piezo:Running", 0.5, false},
		{"GasCell:Piezo:ValveSP", 101.0, false},
		{"GasCell:Vac:A1", 1.0, false},
		{"GasCell:Unknown", 1.0, false},
	} {
		if err := ValidatePVParams(test.name, test.value); (err == nil) != test.ok {
			t.Errorf("ValidatePVParams(%q, %v) error=%v, want ok=%v", test.name, test.value, err, test.ok)
		}
	}
}
