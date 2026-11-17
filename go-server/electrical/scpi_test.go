package electrical

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestRIGOLCollectsAllFourChannels(t *testing.T) {
	device := NewSCPIDevice(DeviceConfig{Source: SourceRIGOL, Addr: "rigol:5555"})
	device.query = func(_ context.Context, _, command string) (string, error) {
		var channel int
		if _, err := fmt.Sscanf(command, ":MEAS:VPP? CHAN%d", &channel); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d.25", channel), nil
	}
	samples, err := device.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 4 {
		t.Fatalf("RIGOL samples = %d, want four Vpp readings", len(samples))
	}
	for index, sample := range samples {
		wantChannel := fmt.Sprintf("CH%d", index+1)
		wantValue := float64(index+1) + 0.25
		if sample.Channel != wantChannel || sample.Metric != "vpp_V" || sample.Unit != "V" || sample.Value == nil || *sample.Value != wantValue {
			t.Errorf("sample %d = %+v, want %s Vpp %.2f V", index, sample, wantChannel, wantValue)
		}
	}
}

func TestTekCollectsFourMeasurementSlotsWithUnits(t *testing.T) {
	device := NewSCPIDevice(DeviceConfig{Source: SourceTek, Addr: "tek:4000"})
	device.query = func(_ context.Context, _, command string) (string, error) {
		var slot int
		if _, err := fmt.Sscanf(command, ":MEASU:MEAS%d:", &slot); err != nil {
			return "", err
		}
		switch {
		case strings.HasSuffix(command, ":TYPE?"):
			return "PK2PK", nil
		case strings.HasSuffix(command, ":SOURCE?"):
			return fmt.Sprintf("CHAN%d", slot), nil
		case strings.HasSuffix(command, ":UNITS?"):
			return `"V"`, nil
		case strings.HasSuffix(command, ":VALUE?"):
			return fmt.Sprintf("%d.75", slot), nil
		default:
			return "", fmt.Errorf("unexpected command %q", command)
		}
	}
	samples, err := device.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]float64)
	for _, sample := range samples {
		if sample.Metric == "measurement_value" && sample.Value != nil {
			values[sample.Channel] = *sample.Value
			if sample.Unit != "V" {
				t.Errorf("%s unit = %q, want V", sample.Channel, sample.Unit)
			}
		}
	}
	if len(values) != 4 {
		t.Fatalf("Tek measurement channels = %v, want CH1..CH4; samples=%+v", values, samples)
	}
	for channel := 1; channel <= 4; channel++ {
		name := fmt.Sprintf("CH%d", channel)
		if values[name] != float64(channel)+0.75 {
			t.Errorf("%s value = %.2f", name, values[name])
		}
	}
}

func TestTekSkipsInvalidMeasurementSentinel(t *testing.T) {
	device := NewSCPIDevice(DeviceConfig{Source: SourceTek, Addr: "tek:4000"})
	device.query = func(_ context.Context, _, command string) (string, error) {
		switch {
		case strings.HasSuffix(command, ":TYPE?"):
			return "PK2PK", nil
		case strings.HasSuffix(command, ":SOURCE?"):
			return "CH1", nil
		case strings.HasSuffix(command, ":UNITS?"):
			return "V", nil
		default:
			return "9.9E37", nil
		}
	}
	samples, err := device.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		if sample.Metric == "measurement_value" {
			t.Fatalf("invalid Tek sentinel became a measurement: %+v", sample)
		}
	}
}
