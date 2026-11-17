package electrical

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	modbusPairTimeout = 2 * time.Second
	maxModbusBuffer   = 64 * 1024
)

type ModbusStats struct {
	CRCErrors      uint64
	DiscardedBytes uint64
	PartialBytes   int
	PairingErrors  uint64
	RequestFrames  uint64
	ResponseFrames uint64
	MappedFrames   uint64
	MaxGap         time.Duration
}

type ModbusFrame struct {
	Time      time.Time
	Kind      string
	Unit      byte
	Function  byte
	Start     uint16
	Count     uint16
	ByteCount byte
	Channel   string
	Mapping   string
	Signature string
	Raw       []byte
	Registers []uint16
}

type modbusRequest struct {
	at    time.Time
	start uint16
	count uint16
}

type modbusRequestKey struct {
	unit     byte
	function byte
}

// ModbusParser incrementally aligns a RTU-over-TCP stream by CRC. It never
// manufactures channel identities: a response becomes mapped only after a
// matching request with a register count and unit/function pair is observed.
type ModbusParser struct {
	buffer     []byte
	pending    map[modbusRequestKey][]modbusRequest
	sawRequest bool
	lastFrame  time.Time
	Stats      ModbusStats
}

func NewModbusParser() *ModbusParser {
	return &ModbusParser{pending: make(map[modbusRequestKey][]modbusRequest)}
}

func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func ValidCRC(frame []byte) bool {
	return len(frame) >= 4 && CRC16(frame[:len(frame)-2]) == binary.LittleEndian.Uint16(frame[len(frame)-2:])
}

func (p *ModbusParser) Feed(data []byte, at time.Time) []ModbusFrame {
	p.buffer = append(p.buffer, data...)
	if len(p.buffer) > maxModbusBuffer {
		drop := len(p.buffer) - 256
		p.Stats.DiscardedBytes += uint64(drop)
		p.buffer = append([]byte(nil), p.buffer[drop:]...)
	}
	var frames []ModbusFrame
	for {
		if len(p.buffer) < 3 {
			break
		}
		unit, fc, third := p.buffer[0], p.buffer[1], p.buffer[2]
		if (unit != 0x14 && unit != 0x15) || (fc != 3 && fc != 4) {
			p.buffer = p.buffer[1:]
			p.Stats.DiscardedBytes++
			continue
		}

		responseLength := 0
		if third >= 2 && third <= 250 && third%2 == 0 {
			responseLength = 5 + int(third)
		}
		if responseLength > 0 && len(p.buffer) >= responseLength {
			candidate := p.buffer[:responseLength]
			if ValidCRC(candidate) {
				raw := append([]byte(nil), candidate...)
				p.buffer = p.buffer[responseLength:]
				frames = append(frames, p.acceptFrame("response", raw, at))
				continue
			}
		}

		requestQuantity := uint16(0)
		if len(p.buffer) >= 8 {
			requestQuantity = binary.BigEndian.Uint16(p.buffer[4:6])
			if requestQuantity >= 1 && requestQuantity <= 125 && ValidCRC(p.buffer[:8]) {
				raw := append([]byte(nil), p.buffer[:8]...)
				p.buffer = p.buffer[8:]
				frames = append(frames, p.acceptFrame("request", raw, at))
				continue
			}
		}

		// A plausible response length means the frame may simply be split across
		// TCP reads. Wait for more bytes instead of misclassifying a partial frame.
		if responseLength > 0 && len(p.buffer) < responseLength {
			break
		}
		if len(p.buffer) < 8 {
			break
		}
		if responseLength > 0 || (requestQuantity >= 1 && requestQuantity <= 125) {
			p.Stats.CRCErrors++
		}
		p.buffer = p.buffer[1:]
		p.Stats.DiscardedBytes++
	}
	p.Stats.PartialBytes = len(p.buffer)
	return frames
}

func (p *ModbusParser) acceptFrame(kind string, raw []byte, at time.Time) ModbusFrame {
	frame := ModbusFrame{Time: at, Kind: kind, Unit: raw[0], Function: raw[1], Mapping: MappingNone, Raw: raw}
	if !p.lastFrame.IsZero() && at.After(p.lastFrame) {
		gap := at.Sub(p.lastFrame)
		if gap > p.Stats.MaxGap {
			p.Stats.MaxGap = gap
		}
	}
	p.lastFrame = at
	if kind == "request" {
		frame.Start = binary.BigEndian.Uint16(raw[2:4])
		frame.Count = binary.BigEndian.Uint16(raw[4:6])
		p.sawRequest = true
		p.Stats.RequestFrames++
		key := modbusRequestKey{unit: frame.Unit, function: frame.Function}
		p.pending[key] = append(p.pending[key], modbusRequest{at: at, start: frame.Start, count: frame.Count})
		return frame
	}

	p.Stats.ResponseFrames++
	frame.ByteCount = raw[2]
	frame.Count = uint16(frame.ByteCount / 2)
	frame.Registers = make([]uint16, frame.Count)
	for i := range frame.Registers {
		frame.Registers[i] = binary.BigEndian.Uint16(raw[3+2*i : 5+2*i])
	}
	frame.Signature = responseSignature(frame.Unit, frame.Function, frame.Registers)
	p.expirePending(at)
	key := modbusRequestKey{unit: frame.Unit, function: frame.Function}
	requests := p.pending[key]
	match := -1
	for i, req := range requests {
		latency := at.Sub(req.at)
		if req.count == frame.Count && latency >= 0 && latency <= modbusPairTimeout {
			match = i
			frame.Start = req.start
			break
		}
	}
	if match >= 0 {
		req := requests[match]
		p.pending[key] = append(requests[:match], requests[match+1:]...)
		if channel, ok := mestekChannel(frame.Unit, req.start); ok {
			frame.Channel = channel
			frame.Mapping = MappingPaired
			p.Stats.MappedFrames++
		}
	} else if p.sawRequest {
		p.Stats.PairingErrors++
	}
	return frame
}

func (p *ModbusParser) expirePending(at time.Time) {
	for key, requests := range p.pending {
		kept := requests[:0]
		for _, req := range requests {
			if at.Sub(req.at) > modbusPairTimeout {
				p.Stats.PairingErrors++
			} else {
				kept = append(kept, req)
			}
		}
		p.pending[key] = kept
	}
}

// Finalize converts outstanding requests into pairing failures at an offline
// capture boundary. It is not used to infer losses in a response-only live stream.
func (p *ModbusParser) Finalize() {
	for key, requests := range p.pending {
		p.Stats.PairingErrors += uint64(len(requests))
		p.pending[key] = nil
	}
	p.Stats.PartialBytes = len(p.buffer)
}

// mestekChannel maps the two physical modules into one unambiguous CH1..CH13
// display space: unit 0x14 owns CH1..CH3; unit 0x15 owns CH4..CH13.
// The register address in the observed request is the only channel evidence.
func mestekChannel(unit byte, start uint16) (string, bool) {
	if start == 0 || start%100 != 0 {
		return "", false
	}
	local := int(start / 100)
	switch unit {
	case 0x14:
		if local >= 1 && local <= 3 {
			return fmt.Sprintf("CH%d", local), true
		}
	case 0x15:
		if local >= 1 && local <= 10 {
			return fmt.Sprintf("CH%d", local+3), true
		}
	}
	return "", false
}

func responseSignature(unit, fc byte, registers []uint16) string {
	static := []uint16(nil)
	if len(registers) >= 15 {
		static = []uint16{registers[4], registers[5], registers[10], registers[12], registers[14]}
	}
	value := fmt.Sprintf("%02x/%02x/%d/%v", unit, fc, len(registers), static)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

// Samples converts only responses to readings. Requests are correlation
// evidence, not measurements. The raw frame remains available even when no
// request was visible and the channel is therefore explicitly UNMAPPED.
func (f ModbusFrame) Samples() []Sample {
	if f.Kind != "response" {
		return nil
	}
	raw := hex.EncodeToString(f.Raw)
	channel := f.Channel
	if channel == "" {
		channel = "UNMAPPED"
	}
	base := Sample{
		Time: f.Time, Source: SourceMESTEK, Channel: channel,
		Mapping: f.Mapping, Signature: f.Signature, Raw: raw, Quality: "good",
	}
	samples := []Sample{}
	if len(f.Registers) >= 16 {
		values := []struct {
			metric string
			unit   string
			value  float64
		}{
			{metric: "output_enabled", value: float64(f.Registers[0])},
			{metric: "U_V", unit: "V", value: float64(f.Registers[6]) / 100},
			{metric: "I_A", unit: "A", value: float64(f.Registers[7]) / 1000},
			{metric: "setU_V", unit: "V", value: float64(f.Registers[10]) / 100},
			// FC03-only readback. The latest field methodology still requires
			// on-site confirmation of +11 semantics before treating it as SetI.
			{metric: "setI_A", unit: "A", value: float64(f.Registers[11]) / 1000},
		}
		for _, item := range values {
			value := item.value
			sample := base
			sample.Metric = item.metric
			sample.Unit = item.unit
			sample.Value = &value
			samples = append(samples, sample)
		}
	}
	frameSample := base
	frameSample.Metric = "frame"
	frameSample.TextValue = raw
	samples = append(samples, frameSample)
	return samples
}
