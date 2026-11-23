package electrical

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sort"
	"time"
)

type PCAPResult struct {
	Frames []ModbusFrame
	Stats  ModbusStats
}

type capturedTCPPacket struct {
	at               time.Time
	srcIP, dstIP     string
	srcPort, dstPort uint16
	seq              uint32
	payload          []byte
}

type tcpDirection struct {
	srcIP, dstIP     string
	srcPort, dstPort uint16
}

type capturedChunk struct {
	at   time.Time
	data []byte
}

// DecodeMESTEKPCAP decodes classic PCAP Ethernet/raw-IP/Linux-SLL TCP captures
// without requiring a live interface. Port 4001 flows are independently
// reassembled in both directions before CRC framing and request/response pairing.
func DecodeMESTEKPCAP(reader io.Reader) (PCAPResult, error) {
	packets, err := readPCAPPackets(reader)
	if err != nil {
		return PCAPResult{}, err
	}
	flows := make(map[string]map[tcpDirection][]capturedTCPPacket)
	for _, packet := range packets {
		if packet.srcPort != 4001 && packet.dstPort != 4001 || len(packet.payload) == 0 {
			continue
		}
		forward := fmt.Sprintf("%s:%d>%s:%d", packet.srcIP, packet.srcPort, packet.dstIP, packet.dstPort)
		reverse := fmt.Sprintf("%s:%d>%s:%d", packet.dstIP, packet.dstPort, packet.srcIP, packet.srcPort)
		flowKey := forward
		if reverse < forward {
			flowKey = reverse
		}
		direction := tcpDirection{srcIP: packet.srcIP, dstIP: packet.dstIP, srcPort: packet.srcPort, dstPort: packet.dstPort}
		if flows[flowKey] == nil {
			flows[flowKey] = make(map[tcpDirection][]capturedTCPPacket)
		}
		flows[flowKey][direction] = append(flows[flowKey][direction], packet)
	}

	flowKeys := make([]string, 0, len(flows))
	for key := range flows {
		flowKeys = append(flowKeys, key)
	}
	sort.Strings(flowKeys)
	var result PCAPResult
	for _, flowKey := range flowKeys {
		var chunks []capturedChunk
		for _, packets := range flows[flowKey] {
			chunks = append(chunks, reassembleTCPChunks(packets)...)
		}
		sort.SliceStable(chunks, func(i, j int) bool { return chunks[i].at.Before(chunks[j].at) })
		parser := NewModbusParser()
		for _, chunk := range chunks {
			parser.Stats.PartialBytes = len(parser.buffer)
			result.Frames = append(result.Frames, parser.Feed(chunk.data, chunk.at)...)
		}
		parser.Finalize()
		result.Stats = mergeModbusStats(result.Stats, parser.Stats)
	}
	sort.SliceStable(result.Frames, func(i, j int) bool { return result.Frames[i].Time.Before(result.Frames[j].Time) })
	return result, nil
}

func mergeModbusStats(a, b ModbusStats) ModbusStats {
	a.CRCErrors += b.CRCErrors
	a.DiscardedBytes += b.DiscardedBytes
	a.PartialBytes += b.PartialBytes
	a.PairingErrors += b.PairingErrors
	a.RequestFrames += b.RequestFrames
	a.ResponseFrames += b.ResponseFrames
	a.MappedFrames += b.MappedFrames
	if b.MaxGap > a.MaxGap {
		a.MaxGap = b.MaxGap
	}
	return a
}

func reassembleTCPChunks(packets []capturedTCPPacket) []capturedChunk {
	sort.SliceStable(packets, func(i, j int) bool {
		if packets[i].seq == packets[j].seq {
			return packets[i].at.Before(packets[j].at)
		}
		return packets[i].seq < packets[j].seq
	})
	chunks := make([]capturedChunk, 0, len(packets))
	var nextSeq uint32
	haveNext := false
	for _, packet := range packets {
		payload := packet.payload
		end := packet.seq + uint32(len(payload))
		if haveNext {
			if end <= nextSeq {
				continue // retransmission or fully overlapping segment
			}
			if packet.seq < nextSeq {
				overlap := nextSeq - packet.seq
				payload = payload[overlap:]
				packet.seq = nextSeq
			}
		}
		if len(payload) == 0 {
			continue
		}
		chunks = append(chunks, capturedChunk{at: packet.at, data: append([]byte(nil), payload...)})
		nextSeq = packet.seq + uint32(len(payload))
		haveNext = true
	}
	return chunks
}

func readPCAPPackets(reader io.Reader) ([]capturedTCPPacket, error) {
	global := make([]byte, 24)
	if _, err := io.ReadFull(reader, global); err != nil {
		return nil, fmt.Errorf("read PCAP header: %w", err)
	}
	var order binary.ByteOrder
	nanoseconds := false
	switch fmt.Sprintf("%x", global[:4]) {
	case "d4c3b2a1":
		order = binary.LittleEndian
	case "a1b2c3d4":
		order = binary.BigEndian
	case "4d3cb2a1":
		order, nanoseconds = binary.LittleEndian, true
	case "a1b23c4d":
		order, nanoseconds = binary.BigEndian, true
	default:
		return nil, fmt.Errorf("unsupported PCAP magic %x", global[:4])
	}
	linkType := order.Uint32(global[20:24])
	var packets []capturedTCPPacket
	for {
		header := make([]byte, 16)
		_, err := io.ReadFull(reader, header)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("truncated PCAP packet header")
		}
		if err != nil {
			return nil, err
		}
		length := order.Uint32(header[8:12])
		if length > 16*1024*1024 {
			return nil, fmt.Errorf("PCAP packet too large: %d", length)
		}
		body := make([]byte, int(length))
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, fmt.Errorf("truncated PCAP packet: %w", err)
		}
		seconds := int64(order.Uint32(header[0:4]))
		fraction := int64(order.Uint32(header[4:8]))
		if !nanoseconds {
			fraction *= 1000
		}
		packet, ok, err := parseCapturedPacket(linkType, body, time.Unix(seconds, fraction).UTC())
		if err != nil {
			return nil, err
		}
		if ok {
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func parseCapturedPacket(linkType uint32, body []byte, at time.Time) (capturedTCPPacket, bool, error) {
	var ip []byte
	switch linkType {
	case 1: // Ethernet, including one or more VLAN tags
		if len(body) < 14 {
			return capturedTCPPacket{}, false, nil
		}
		offset := 12
		etherType := binary.BigEndian.Uint16(body[offset : offset+2])
		offset += 2
		for etherType == 0x8100 || etherType == 0x88A8 {
			if len(body) < offset+4 {
				return capturedTCPPacket{}, false, nil
			}
			etherType = binary.BigEndian.Uint16(body[offset+2 : offset+4])
			offset += 4
		}
		if etherType != 0x0800 {
			return capturedTCPPacket{}, false, nil
		}
		ip = body[offset:]
	case 101: // RAW IPv4
		ip = body
	case 113: // Linux cooked capture v1
		if len(body) < 16 || binary.BigEndian.Uint16(body[14:16]) != 0x0800 {
			return capturedTCPPacket{}, false, nil
		}
		ip = body[16:]
	default:
		return capturedTCPPacket{}, false, fmt.Errorf("unsupported PCAP link type %d", linkType)
	}
	if len(ip) < 20 || ip[0]>>4 != 4 {
		return capturedTCPPacket{}, false, nil
	}
	headerLength := int(ip[0]&0x0F) * 4
	if headerLength < 20 || len(ip) < headerLength {
		return capturedTCPPacket{}, false, nil
	}
	totalLength := int(binary.BigEndian.Uint16(ip[2:4]))
	if totalLength < headerLength+20 || totalLength > len(ip) {
		return capturedTCPPacket{}, false, nil
	}
	if ip[9] != 6 || binary.BigEndian.Uint16(ip[6:8])&0x1FFF != 0 {
		return capturedTCPPacket{}, false, nil
	}
	tcp := ip[headerLength:totalLength]
	tcpHeaderLength := int(tcp[12]>>4) * 4
	if tcpHeaderLength < 20 || len(tcp) < tcpHeaderLength {
		return capturedTCPPacket{}, false, nil
	}
	return capturedTCPPacket{
		at: at, srcIP: net.IP(ip[12:16]).String(), dstIP: net.IP(ip[16:20]).String(),
		srcPort: binary.BigEndian.Uint16(tcp[0:2]), dstPort: binary.BigEndian.Uint16(tcp[2:4]),
		seq: binary.BigEndian.Uint32(tcp[4:8]), payload: append([]byte(nil), tcp[tcpHeaderLength:]...),
	}, true, nil
}
