package runs

import (
	"errors"
	"testing"
)

func TestTargetTransition(t *testing.T) {
	tests := []struct {
		status, action, want string
		started, ended       bool
	}{
		{StatusPlanned, "start", StatusActive, true, false},
		{StatusPlanned, "abort", StatusAborted, false, true},
		{StatusActive, "pause", StatusPaused, true, false},
		{StatusActive, "complete", StatusCompleted, true, true},
		{StatusActive, "abort", StatusAborted, true, true},
		{StatusPaused, "resume", StatusActive, true, false},
		{StatusPaused, "abort", StatusAborted, true, true},
	}
	for _, tt := range tests {
		got, started, ended, err := targetTransition(tt.status, tt.action)
		if err != nil || got != tt.want || started != tt.started || ended != tt.ended {
			t.Errorf("targetTransition(%q, %q) = %q, %v, %v, %v", tt.status, tt.action, got, started, ended, err)
		}
	}
	if _, _, _, err := targetTransition(StatusCompleted, "resume"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("invalid transition error = %v", err)
	}
}
