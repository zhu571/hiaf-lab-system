package electrical

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestKeithleyActivePollUsesReadOnlyCommands(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	cfg.Enabled = false
	svc := NewService(cfg, nil)
	commands := make(chan string, 8)
	svc.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				command := strings.TrimSpace(line)
				commands <- command
				if command == ":READ?" {
					_, _ = server.Write([]byte(":READ?\n+7.919254E-10A,+1.011508E+06,+2.000000E+00\n"))
				}
			}
		}()
		return client, nil
	}
	samples, err := svc.collectActiveKeithley(context.Background(), &KeithleyParser{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Value == nil {
		t.Fatalf("samples = %+v, want one numeric sample", samples)
	}
	if samples[0].Source != Source6485 || samples[0].Channel != "CH1" || samples[0].Metric != "current_A" {
		t.Fatalf("sample identity = (%s, %s, %s), want (%s, CH1, current_A)", samples[0].Source, samples[0].Channel, samples[0].Metric, Source6485)
	}
	if got := *samples[0].Value; got < 7.91e-10 || got > 7.92e-10 {
		t.Fatalf("parsed value = %v, want ~7.919e-10", got)
	}
	if samples[0].Raw != "+7.919254E-10A,+1.011508E+06,+2.000000E+00" {
		t.Fatalf("raw = %q, want the matched reading line", samples[0].Raw)
	}
	close(commands)
	var seen []string
	for command := range commands {
		seen = append(seen, command)
	}
	if len(seen) != 2 || seen[0] != ":ARM:COUNT 1" || seen[1] != ":READ?" {
		t.Fatalf("commands = %v, want only arm + read", seen)
	}
}

func TestKeithleyActiveRejectsUnrecognizedResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	svc.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			for {
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
				_, _ = server.Write([]byte("garbage\n"))
			}
		}()
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := svc.collectActiveKeithley(ctx, &KeithleyParser{}); err == nil {
		t.Fatal("unrecognized response was accepted")
	}
}

func TestKeithleyCommandWhitelist(t *testing.T) {
	for _, command := range []string{":ARM:COUNT 1", ":READ?"} {
		if !allowedKeithleyCommand(command) {
			t.Fatalf("%q should be allowed", command)
		}
	}
	for _, command := range []string{":OUTP ON", ":CURR:DC", ":SOUR:VOLT 10", ":ARM:COUNT 0"} {
		if allowedKeithleyCommand(command) {
			t.Fatalf("%q must be rejected", command)
		}
	}
}
