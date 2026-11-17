package electrical

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var scpiCommands = map[string][]string{
	SourceRIGOL: {
		":MEAS:VPP? CHAN1", ":MEAS:VPP? CHAN2", ":MEAS:VPP? CHAN3", ":MEAS:VPP? CHAN4",
	},
	SourceTek: {
		":MEASU:MEAS1:TYPE?", ":MEASU:MEAS1:SOURCE?", ":MEASU:MEAS1:VALUE?",
	},
	Source33512B: {
		":SOUR1:APPL?", ":OUTP1?", ":SOUR2:APPL?", ":OUTP2?",
	},
	Source33210A: {
		":APPL?", ":OUTP?",
	},
}

type queryFunc func(ctx context.Context, addr, command string) (string, error)

type SCPIDevice struct {
	cfg   DeviceConfig
	query queryFunc
	mu    sync.Mutex
}

func NewSCPIDevice(cfg DeviceConfig) *SCPIDevice {
	return &SCPIDevice{cfg: cfg, query: querySCPI}
}

func (d *SCPIDevice) Collect(ctx context.Context) ([]Sample, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	commands := scpiCommands[d.cfg.Source]
	if len(commands) == 0 {
		return nil, fmt.Errorf("unknown SCPI source %q", d.cfg.Source)
	}
	var samples []Sample
	var errs []string
	ask := func(command string) (string, bool) {
		response, err := d.query(ctx, d.cfg.Addr, command)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", command, err))
			return "", false
		}
		if strings.TrimSpace(response) == "" {
			errs = append(errs, command+": empty response")
			return "", false
		}
		return response, true
	}

	switch d.cfg.Source {
	case SourceRIGOL:
		for channel, command := range commands {
			response, ok := ask(command)
			if !ok {
				continue
			}
			value, err := parseSCPIFloat(response)
			if err != nil {
				errs = append(errs, command+": "+err.Error())
				continue
			}
			samples = append(samples, numericSample(d.cfg.Source, fmt.Sprintf("CH%d", channel+1), "vpp_V", "V", value, response))
		}
	case SourceTek:
		var typ, source, rawValue string
		var ok bool
		if typ, ok = ask(commands[0]); !ok {
			typ = ""
		}
		if source, ok = ask(commands[1]); !ok {
			source = "MEAS1"
		}
		if rawValue, ok = ask(commands[2]); ok {
			value, err := parseSCPIFloat(rawValue)
			if err != nil {
				errs = append(errs, commands[2]+": "+err.Error())
			} else {
				channel := strings.Trim(strings.TrimSpace(source), "\"")
				if channel == "" {
					channel = "MEAS1"
				}
				unit := ""
				if strings.EqualFold(strings.TrimSpace(typ), "PK2PK") {
					unit = "V"
				}
				samples = append(samples, numericSample(d.cfg.Source, channel, "measurement_value", unit, value, rawValue))
			}
		}
		if typ != "" {
			samples = append(samples, textSample(d.cfg.Source, "MEAS1", "type", typ, typ))
		}
		if source != "" {
			samples = append(samples, textSample(d.cfg.Source, "MEAS1", "source", source, source))
		}
	case Source33512B:
		for channel := 1; channel <= 2; channel++ {
			applyCommand := commands[(channel-1)*2]
			outputCommand := commands[(channel-1)*2+1]
			if response, ok := ask(applyCommand); ok {
				samples = append(samples, parseApply(d.cfg.Source, fmt.Sprintf("CH%d", channel), response)...)
			}
			if response, ok := ask(outputCommand); ok {
				value, err := parseSCPIFloat(response)
				if err != nil {
					errs = append(errs, outputCommand+": "+err.Error())
				} else {
					samples = append(samples, numericSample(d.cfg.Source, fmt.Sprintf("CH%d", channel), "output_enabled", "", value, response))
				}
			}
		}
	case Source33210A:
		if response, ok := ask(commands[0]); ok {
			samples = append(samples, parseApply(d.cfg.Source, "CH1", response)...)
		}
		if response, ok := ask(commands[1]); ok {
			value, err := parseSCPIFloat(response)
			if err != nil {
				errs = append(errs, commands[1]+": "+err.Error())
			} else {
				samples = append(samples, numericSample(d.cfg.Source, "CH1", "output_enabled", "", value, response))
			}
		}
	}
	if len(errs) > 0 {
		return samples, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return samples, nil
}

func querySCPI(ctx context.Context, addr, command string) (string, error) {
	if !allowedSCPICommand(command) {
		return "", fmt.Errorf("SCPI command is not in the fixed read-only whitelist")
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write([]byte(command + "\n")); err != nil {
		return "", err
	}
	reader := bufio.NewReader(io.LimitReader(conn, 4096))
	raw, err := reader.ReadString('\n')
	if err != nil && len(raw) == 0 {
		return "", err
	}
	if len(raw) >= 4096 && !strings.Contains(raw, "\n") {
		return "", fmt.Errorf("SCPI response exceeds 4096 bytes")
	}
	return strings.TrimSpace(strings.ReplaceAll(raw, "\r", "")), nil
}

func allowedSCPICommand(command string) bool {
	for _, commands := range scpiCommands {
		for _, allowed := range commands {
			if command == allowed {
				return true
			}
		}
	}
	return false
}

func parseSCPIFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.Trim(strings.TrimSpace(raw), "\""), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric response %q", raw)
	}
	return value, nil
}

func parseApply(source, channel, response string) []Sample {
	body := strings.Trim(strings.TrimSpace(response), "\"")
	parts := strings.Split(body, ",")
	if len(parts) == 0 {
		return nil
	}
	head := strings.Fields(strings.TrimSpace(parts[0]))
	samples := []Sample{}
	if len(head) > 0 {
		samples = append(samples, textSample(source, channel, "waveform", head[0], response))
	}
	// Keysight APPL? returns "SIN <frequency>,<amplitude>,<offset>":
	// frequency shares the first comma field with the waveform.
	if len(head) > 1 {
		if value, err := parseSCPIFloat(head[len(head)-1]); err == nil {
			samples = append(samples, numericSample(source, channel, "frequency_Hz", "Hz", value, response))
		}
	}
	for index, metric := range []string{"amplitude_V", "offset_V"} {
		if len(parts) <= index+1 {
			continue
		}
		if value, err := parseSCPIFloat(parts[index+1]); err == nil {
			samples = append(samples, numericSample(source, channel, metric, "V", value, response))
		}
	}
	return samples
}

func numericSample(source, channel, metric, unit string, value float64, raw string) Sample {
	return Sample{
		Time: time.Now(), Source: source, Channel: channel, Metric: metric, Unit: unit,
		Mapping: MappingDirect, Value: &value, Raw: raw, Quality: "good",
	}
}

func textSample(source, channel, metric, value, raw string) Sample {
	return Sample{
		Time: time.Now(), Source: source, Channel: channel, Metric: metric,
		Mapping: MappingDirect, TextValue: value, Raw: raw, Quality: "good",
	}
}
