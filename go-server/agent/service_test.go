package agent

import (
	"strings"
	"testing"
)

func TestSanitizeError(t *testing.T) {
	if got := sanitizeError("request failed: Bearer secret"); !strings.Contains(got, "redacted") {
		t.Fatalf("expected redaction, got %q", got)
	}
	if got := sanitizeError(strings.Repeat("x", 600)); len([]rune(got)) != 512 {
		t.Fatalf("expected 512 runes, got %d", len([]rune(got)))
	}
}

func TestValidActionType(t *testing.T) {
	if !validActionType("create_issue") || validActionType("delete_issue") {
		t.Fatal("candidate action allowlist is wrong")
	}
}
