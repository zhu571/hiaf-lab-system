package electrical

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func appendPCAPPacket(buffer *bytes.Buffer, order binary.ByteOrder, at time.Time, srcIP, dstIP [4]byte, srcPort, dstPort uint16, seq uint32, payload []byte) {
	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:2], srcPort)
	binary.BigEndian.PutUint16(tcp[2:4], dstPort)
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	tcp[12] = 5 << 4
	tcp[13] = 0x18
	binary.BigEndian.PutUint16(tcp[14:16], 65535)

	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+len(tcp)+len(payload)))
	ip[8] = 64
	ip[9] = 6
	copy(ip[12:16], srcIP[:])
	copy(ip[16:20], dstIP[:])

	ethernet := make([]byte, 14)
	binary.BigEndian.PutUint16(ethernet[12:14], 0x0800)
	body := append(ethernet, ip...)
	body = append(body, tcp...)
	body = append(body, payload...)

	header := make([]byte, 16)
	order.PutUint32(header[0:4], uint32(at.Unix()))
	order.PutUint32(header[4:8], uint32(at.Nanosecond()/1000))
	order.PutUint32(header[8:12], uint32(len(body)))
	order.PutUint32(header[12:16], uint32(len(body)))
	_ = binary.Write(buffer, order, header)
	_ = binary.Write(buffer, order, body)
}

func TestDecodeMESTEKPCAPBidirectionalAndMissingResponse(t *testing.T) {
	var capture bytes.Buffer
	order := binary.LittleEndian
	_ = binary.Write(&capture, order, []byte{0xd4, 0xc3, 0xb2, 0xa1, 2, 0, 4, 0})
	_ = binary.Write(&capture, order, make([]byte, 8))
	_ = binary.Write(&capture, order, uint32(65535))
	_ = binary.Write(&capture, order, uint32(1))

	client := [4]byte{10, 0, 0, 10}
	server := [4]byte{10, 0, 0, 20}
	start := time.Unix(5000, 0)
	requestSeq := uint32(1000)
	responseSeq := uint32(9000)
	for channel := 1; channel <= 13; channel++ {
		at := start.Add(time.Duration(channel) * 30 * time.Millisecond)
		request := testRequest(0x14, uint16(channel*100), 20)
		// Split one request across TCP segments to exercise reassembly.
		if channel == 1 {
			appendPCAPPacket(&capture, order, at, client, server, 51000, 4001, requestSeq, request[:3])
			requestSeq += 3
			appendPCAPPacket(&capture, order, at.Add(time.Millisecond), client, server, 51000, 4001, requestSeq, request[3:])
			requestSeq += uint32(len(request) - 3)
		} else {
			appendPCAPPacket(&capture, order, at, client, server, 51000, 4001, requestSeq, request)
			requestSeq += uint32(len(request))
		}
		if channel == 13 {
			continue // missing final response must remain detectable at capture end
		}
		response := testResponse(0x14, testRegisters())
		appendPCAPPacket(&capture, order, at.Add(10*time.Millisecond), server, client, 4001, 51000, responseSeq, response)
		responseSeq += uint32(len(response))
	}

	result, err := DecodeMESTEKPCAP(bytes.NewReader(capture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.RequestFrames != 13 || result.Stats.ResponseFrames != 12 || result.Stats.MappedFrames != 12 {
		t.Fatalf("unexpected PCAP stats: %+v", result.Stats)
	}
	if result.Stats.PairingErrors != 1 {
		t.Fatalf("missing final response not detected: %+v", result.Stats)
	}
	seen := map[string]bool{}
	for _, frame := range result.Frames {
		if frame.Kind == "response" {
			seen[frame.Channel] = true
		}
	}
	if len(seen) != 12 || seen["CH13"] {
		t.Fatalf("unexpected channel mapping: %v", seen)
	}
}

func TestDecodeMESTEKPCAPResponseOnly(t *testing.T) {
	var capture bytes.Buffer
	order := binary.BigEndian
	_ = binary.Write(&capture, order, []byte{0xa1, 0xb2, 0xc3, 0xd4, 0, 2, 0, 4})
	_ = binary.Write(&capture, order, make([]byte, 8))
	_ = binary.Write(&capture, order, uint32(65535))
	_ = binary.Write(&capture, order, uint32(1))
	response := testResponse(0x15, testRegisters())
	appendPCAPPacket(&capture, order, time.Unix(6000, 0), [4]byte{10, 0, 0, 20}, [4]byte{10, 0, 0, 10}, 4001, 51000, 1, response)
	result, err := DecodeMESTEKPCAP(bytes.NewReader(capture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.RequestFrames != 0 || result.Stats.MappedFrames != 0 || result.Stats.PairingErrors != 0 {
		t.Fatalf("response-only capture invented mapping/errors: %+v", result.Stats)
	}
	if len(result.Frames) != 1 || result.Frames[0].Mapping != MappingNone {
		t.Fatalf("response-only frames: %+v", result.Frames)
	}
}
