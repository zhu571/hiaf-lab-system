package electrical

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const numberPattern = `[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`

var keithleyLinePattern = regexp.MustCompile(`^\s*(` + numberPattern + `)A\s*,\s*(` + numberPattern + `)\s*,\s*(` + numberPattern + `)\s*$`)

type KeithleyParser struct {
	buffer       []byte
	ParseErrors  uint64
	PartialBytes int
	MaxGap       time.Duration
	lastSample   time.Time
}

func (p *KeithleyParser) Feed(data []byte, at time.Time) []Sample {
	p.buffer = append(p.buffer, data...)
	if len(p.buffer) > 64*1024 {
		p.ParseErrors++
		p.buffer = append([]byte(nil), p.buffer[len(p.buffer)-1024:]...)
	}
	var samples []Sample
	for {
		index := bytes.IndexAny(p.buffer, "\r\n")
		if index < 0 {
			break
		}
		line := strings.TrimSpace(string(p.buffer[:index]))
		p.buffer = p.buffer[index+1:]
		if line == "" {
			continue
		}
		match := keithleyLinePattern.FindStringSubmatch(line)
		if match == nil {
			p.ParseErrors++
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			p.ParseErrors++
			continue
		}
		if !p.lastSample.IsZero() && at.After(p.lastSample) {
			gap := at.Sub(p.lastSample)
			if gap > p.MaxGap {
				p.MaxGap = gap
			}
		}
		p.lastSample = at
		sample := numericSample(Source6485, "-", "current_A", "A", value, line)
		sample.Time = at
		sample.Mapping = MappingDirect
		samples = append(samples, sample)
	}
	p.PartialBytes = len(p.buffer)
	return samples
}

func ParseKeithleyLine(line string, at time.Time) (Sample, error) {
	parser := &KeithleyParser{}
	samples := parser.Feed([]byte(line+"\n"), at)
	if len(samples) != 1 {
		return Sample{}, fmt.Errorf("invalid Keithley 6485 line %q", line)
	}
	return samples[0], nil
}
