package instruments

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"

	"gopkg.in/yaml.v3"
)

//go:embed gascell_pv_ranges.yaml
var gasCellPVYAML []byte

type PVRange struct {
	Type              string  `yaml:"type" json:"type"`
	Min               float64 `yaml:"min" json:"min"`
	Max               float64 `yaml:"max" json:"max"`
	Unit              string  `yaml:"unit" json:"unit"`
	ReadbackTolerance float64 `yaml:"readback_tolerance" json:"readback_tolerance"`
	Risk              string  `yaml:"risk" json:"risk"`
	Writable          bool    `yaml:"writable" json:"writable"`
}

type pvRangeFile struct {
	Version string             `yaml:"version"`
	PVs     map[string]PVRange `yaml:"pvs"`
}

var gasCellPVConfig = loadGasCellPVConfig()

func loadGasCellPVConfig() map[string]PVRange {
	var file pvRangeFile
	if err := yaml.Unmarshal(gasCellPVYAML, &file); err != nil {
		panic(fmt.Errorf("parse GasCell PV config: %w", err))
	}
	if file.Version == "" || len(file.PVs) == 0 {
		panic("GasCell PV config requires version and pvs")
	}
	for name, cfg := range file.PVs {
		if name == "" || (cfg.Type != "float" && cfg.Type != "int" && cfg.Type != "string") || cfg.Unit == "" || (cfg.Risk != "green" && cfg.Risk != "yellow" && cfg.Risk != "red") || cfg.Min > cfg.Max || cfg.ReadbackTolerance < 0 {
			panic(fmt.Sprintf("GasCell PV config entry %q is invalid", name))
		}
	}
	return file.PVs
}

// ValidatePVParams validates a write value against the embedded GasCell PV contract.
func ValidatePVParams(name string, value any) error {
	cfg, ok := gasCellPVConfig[name]
	if !ok {
		return fmt.Errorf("PV %q is not configured", name)
	}
	if !cfg.Writable {
		return fmt.Errorf("PV %q is read-only", name)
	}
	if cfg.Type == "string" {
		s, ok := value.(string)
		if !ok || float64(len(s)) < cfg.Min || float64(len(s)) > cfg.Max {
			return fmt.Errorf("PV %q requires a string length between %.0f and %.0f", name, cfg.Min, cfg.Max)
		}
		return nil
	}
	n, ok := numericValue(value)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < cfg.Min || n > cfg.Max || (cfg.Type == "int" && math.Trunc(n) != n) {
		return fmt.Errorf("PV %q requires %s between %g and %g", name, cfg.Type, cfg.Min, cfg.Max)
	}
	return nil
}

func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
