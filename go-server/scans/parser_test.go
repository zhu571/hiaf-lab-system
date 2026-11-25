package scans

import (
	"strings"
	"testing"
)

func TestParseTXTV7(t *testing.T) {
	data := "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\r\n" +
		"0.05\t-12.929\t3.341\t2.480\t1.045\t1.851\t2.165\r\n" +
		"0.10\t-13.001\t3.300\t2.470\t1.040\t1.840\t2.160\r\n"
	res := ParseTXT([]byte(data))
	if res.Format != FormatV7 {
		t.Fatalf("format = %q, want v7", res.Format)
	}
	if len(res.Columns) != 7 || res.Columns[0] != "ref" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if res.RowCount != 2 || res.BadRows != 0 {
		t.Fatalf("rows=%d bad=%d", res.RowCount, res.BadRows)
	}
	if res.Rows[0][0] == nil || *res.Rows[0][0] != 0.05 {
		t.Fatalf("first cell = %v", res.Rows[0][0])
	}
}

func TestParseTXTBadRowsAndEmptyCells(t *testing.T) {
	data := "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\n" +
		"0.05\t-12.9\t3.3\t2.4\t1.0\t1.8\t2.1\n" +
		"0.10\tnope\t3.3\t2.4\t1.0\t1.8\t2.1\n" +
		"0.15\t\t3.3\t2.4\t1.0\t1.8\t2.1\n"
	res := ParseTXT([]byte(data))
	if res.RowCount != 2 || res.BadRows != 1 {
		t.Fatalf("rows=%d bad=%d, want 2/1", res.RowCount, res.BadRows)
	}
	if res.Rows[1][1] != nil {
		t.Fatalf("empty cell must be nil, got %v", *res.Rows[1][1])
	}
}

func TestParseTXTFormatsAndBOM(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"v7", "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4", FormatV7},
		{"v11", "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\tTVpp1\tTVpp2\tTVpp3\tTVpp4", FormatV11},
		{"v12", "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\tTVpp1\tTVpp2\tTVpp3\tTVpp4\textra", FormatV12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := "\xef\xbb\xbf" + tc.header + "\r\n0.1\t1\t2\t3\t4\t5\t6\t7\t8\t9\t10\t11\r\n"
			res := ParseTXT([]byte(data))
			if res.Format != tc.want {
				t.Fatalf("format = %q, want %q", res.Format, tc.want)
			}
		})
	}
}

func TestParseTXTLegacyAndUnknown(t *testing.T) {
	legacy := ParseTXT([]byte("1\t2\t3\n4\t5\t6\n"))
	if legacy.Format != FormatLegacy3 || legacy.RowCount != 2 {
		t.Fatalf("legacy format=%q rows=%d", legacy.Format, legacy.RowCount)
	}
	if len(legacy.Rows) != 0 {
		t.Fatal("legacy must not expose chart rows")
	}

	unknown := ParseTXT([]byte("1\t2\t3\t4\n5\t6\t7\t8\n"))
	if unknown.Format != FormatUnknown {
		t.Fatalf("unknown format = %q", unknown.Format)
	}
	if !containsWarning(unknown.Warnings, "unrecognized_format") {
		t.Fatalf("warnings = %v", unknown.Warnings)
	}

	badUTF8 := ParseTXT([]byte{0xff, 0xfe, 0x00})
	if badUTF8.Format != FormatUnknown || !containsWarning(badUTF8.Warnings, "invalid_utf8") {
		t.Fatalf("invalid utf8: format=%q warnings=%v", badUTF8.Format, badUTF8.Warnings)
	}
}

func TestParseTXTTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString("ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\n")
	for i := 0; i < MaxParseRows+10; i++ {
		b.WriteString("0.1\t1\t2\t3\t4\t5\t6\n")
	}
	res := ParseTXT([]byte(b.String()))
	if !res.Truncated || res.RowCount != MaxParseRows {
		t.Fatalf("truncated=%v rows=%d", res.Truncated, res.RowCount)
	}
}

func containsWarning(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
