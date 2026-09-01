package logs

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildLogWhereKeyword(t *testing.T) {
	where, args := buildLogWhere("project-1", LogListParams{Keyword: "  冷头  ", Status: LogStatusConfirmed})
	if !strings.Contains(where, "content ILIKE $2") || !strings.Contains(where, "content_status = $3") {
		t.Fatalf("unexpected where: %s", where)
	}
	want := []any{"project-1", "%冷头%", LogStatusConfirmed}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildReportWhereDateRange(t *testing.T) {
	where, args := buildReportWhere(ReportListParams{AuthorID: "user-1", DateFrom: "2026-08-01", DateTo: "2026-08-31"})
	if !strings.Contains(where, "dr.report_date >= $2::date") || !strings.Contains(where, "dr.report_date <= $3::date") {
		t.Fatalf("unexpected where: %s", where)
	}
	want := []any{"user-1", "2026-08-01", "2026-08-31"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}
