package instruments

import "testing"

func TestNormalizeParamsRejectsUnsafeValues(t *testing.T) {
	if _, err := NormalizeParams("e5063a", "set_power", map[string]any{"power_dbm": 99.0}); err == nil {
		t.Fatal("expected out-of-range power to be rejected")
	}
	if _, err := NormalizeParams("e5063a", "set_power", map[string]any{"power_dbm": "-30;*RST"}); err == nil {
		t.Fatal("expected string injection to be rejected")
	}
	if _, err := NormalizeParams("e5063a", "take_screenshot", map[string]any{"label": "../bad"}); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestRenderSCPIAppliesDefaultsAndObjectConstraints(t *testing.T) {
	scpi, params, err := RenderSCPI("keysight_33210a", "set_output_voltage", map[string]any{"voltage_vpp": 5.0})
	if err != nil || scpi != "VOLTage:UNIT VPP;\nVOLTage 5" || params["voltage_vpp"] != 5.0 {
		t.Fatalf("unexpected rendered command scpi=%q params=%v err=%v", scpi, params, err)
	}
	if _, _, err := RenderSCPI("e5063a", "set_power", map[string]any{"power_dbm": -20.0}); err == nil {
		t.Fatal("unknown object defaults must apply the conservative -30 dBm ceiling")
	}
}
