package electrical

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func testCRCFrame(payload []byte) []byte {
	frame := append([]byte(nil), payload...)
	crc := CRC16(frame)
	return binary.LittleEndian.AppendUint16(frame, crc)
}

func testRequest(unit byte, start, count uint16) []byte {
	payload := []byte{unit, 3, byte(start >> 8), byte(start), byte(count >> 8), byte(count)}
	return testCRCFrame(payload)
}

func testResponse(unit byte, registers []uint16) []byte {
	payload := []byte{unit, 3, byte(len(registers) * 2)}
	for _, register := range registers {
		payload = append(payload, byte(register>>8), byte(register))
	}
	return testCRCFrame(payload)
}

func testRegisters() []uint16 {
	registers := make([]uint16, 20)
	registers[0] = 1
	registers[4] = 3200
	registers[5] = 5100
	registers[6] = 680
	registers[7] = 2388
	registers[10] = 680
	registers[11] = 3000
	registers[12] = 6000
	registers[14] = 100
	return registers
}

func TestModbusResponseOnlyStaysUnmapped(t *testing.T) {
	raw, err := hex.DecodeString("14032800010028023311080c8013ec03e8095400005d4803e80bb8000000000000000000000000000000005822")
	if err != nil {
		t.Fatal(err)
	}
	parser := NewModbusParser()
	at := time.Unix(100, 0)
	var frames []ModbusFrame
	for _, b := range raw {
		frames = append(frames, parser.Feed([]byte{b}, at)...)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	frame := frames[0]
	if frame.Mapping != MappingNone || frame.Channel != "" || frame.Signature == "" {
		t.Fatalf("response-only frame acquired an identity: %+v", frame)
	}
	samples := frame.Samples()
	if len(samples) != 6 {
		t.Fatalf("samples = %d, want 5 readings + raw frame", len(samples))
	}
	for _, sample := range samples {
		if sample.Channel != "UNMAPPED" || sample.Mapping != MappingNone || sample.Raw == "" {
			t.Fatalf("unmapped sample lost provenance: %+v", sample)
		}
	}
}

func TestModbusMapsThirteenChannelsFromRequests(t *testing.T) {
	parser := NewModbusParser()
	start := time.Unix(1000, 0)
	channels := map[string]bool{}
	var frames []ModbusFrame
	requests := []struct {
		unit     byte
		channels int
	}{{unit: 0x14, channels: 3}, {unit: 0x15, channels: 10}}
	sequence := 0
	for _, module := range requests {
		for local := 1; local <= module.channels; local++ {
			sequence++
			at := start.Add(time.Duration(sequence) * 20 * time.Millisecond)
			request := testRequest(module.unit, uint16(local*100), 20)
			response := testResponse(module.unit, testRegisters())
			frames = append(frames, parser.Feed(request, at)...)
			frames = append(frames, parser.Feed(response[:7], at.Add(time.Millisecond))...)
			frames = append(frames, parser.Feed(response[7:], at.Add(2*time.Millisecond))...)
		}
	}
	for _, frame := range frames {
		if frame.Kind == "response" {
			if frame.Mapping != MappingPaired {
				t.Fatalf("response not paired: %+v", frame)
			}
			channels[frame.Channel] = true
		}
	}
	for channel := 1; channel <= 13; channel++ {
		name := fmt.Sprintf("CH%d", channel)
		if !channels[name] {
			t.Errorf("missing %s: %v", name, channels)
		}
	}
	if len(channels) != 13 || parser.Stats.MappedFrames != 13 || parser.Stats.PairingErrors != 0 {
		t.Fatalf("channels=%v stats=%+v", channels, parser.Stats)
	}
}

func TestModbusDetectsOutstandingRequestAndBadCRC(t *testing.T) {
	parser := NewModbusParser()
	at := time.Unix(2000, 0)
	parser.Feed(testRequest(0x14, 100, 20), at)
	frames := parser.Feed(testResponse(0x14, testRegisters()), at.Add(time.Millisecond))
	if len(frames) != 1 || frames[0].Channel != "CH1" {
		t.Fatalf("first pair failed: %+v", frames)
	}
	parser.Feed(testRequest(0x14, 200, 20), at.Add(2*time.Millisecond))
	parser.Finalize()
	if parser.Stats.PairingErrors != 1 {
		t.Fatalf("missing response not detected: %+v", parser.Stats)
	}

	corrupt := testResponse(0x14, testRegisters())
	corrupt[len(corrupt)-1] ^= 0xFF
	parser.Feed(corrupt[:5], at.Add(time.Second))
	if parser.Stats.PartialBytes == 0 {
		t.Fatal("partial frame not tracked")
	}
	parser.Feed(corrupt[5:], at.Add(time.Second))
	if parser.Stats.CRCErrors == 0 {
		t.Fatal("bad CRC not reported")
	}
}

func TestKeithleyAndApplyParsing(t *testing.T) {
	at := time.Unix(3000, 0)
	sample, err := ParseKeithleyLine("+7.919254E-10A,+1.011508E+06,+2.000000E+00", at)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Value == nil || *sample.Value != 7.919254e-10 || sample.Unit != "A" {
		t.Fatalf("6485 sample: %+v", sample)
	}
	if _, err := ParseKeithleyLine("not a reading", at); err == nil {
		t.Fatal("invalid 6485 line accepted")
	}

	samples := parseApply(Source33210A, "CH1", `"SIN +7.3100000000000E+06,+2.4000000000000E+00,+0.0000000000000E+00"`)
	if len(samples) != 4 {
		t.Fatalf("APPL samples = %d, want 4", len(samples))
	}
	if samples[0].TextValue != "SIN" || *samples[1].Value != 7.31e6 || *samples[2].Value != 2.4 || *samples[3].Value != 0 {
		t.Fatalf("APPL parsing: %+v", samples)
	}
}

func TestSCPIWhitelistRejectsWrites(t *testing.T) {
	if allowedSCPICommand(":OUTP1 ON") || allowedSCPICommand("*RST") || allowedSCPICommand(":MEAS:VPP? CHAN5") {
		t.Fatal("non-whitelisted command accepted")
	}
	for source, commands := range scpiCommands {
		for _, command := range commands {
			if !allowedSCPICommand(command) {
				t.Fatalf("fixed query rejected for %s: %s", source, command)
			}
		}
	}
	if _, err := querySCPI(nil, "127.0.0.1:1", ":OUTP1 ON"); err == nil {
		t.Fatal("write command reached network query path")
	}
}
