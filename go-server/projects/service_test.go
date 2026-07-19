package projects

import (
	"errors"
	"testing"
)

func TestTargetStatus(t *testing.T) {
	tests := []struct {
		name    string
		current string
		action  string
		want    string
		wantErr error
	}{
		{name: "activate from draft", current: StatusDraft, action: "activate", want: StatusActive},
		{name: "complete from active", current: StatusActive, action: "complete", want: StatusCompleted},
		{name: "archive from completed", current: StatusCompleted, action: "archive", want: StatusArchived},
		{name: "reactivate from archived", current: StatusArchived, action: "reactivate", want: StatusActive},
		{name: "deactivate from active", current: StatusActive, action: "deactivate", want: StatusDraft},
		{name: "reopen from completed", current: StatusCompleted, action: "reopen", want: StatusActive},
		{name: "deactivate from draft rejected", current: StatusDraft, action: "deactivate", wantErr: ErrInvalidTransition},
		{name: "deactivate from completed rejected", current: StatusCompleted, action: "deactivate", wantErr: ErrInvalidTransition},
		{name: "reopen from active rejected", current: StatusActive, action: "reopen", wantErr: ErrInvalidTransition},
		{name: "reopen from archived rejected", current: StatusArchived, action: "reopen", wantErr: ErrInvalidTransition},
		{name: "unknown action rejected", current: StatusActive, action: "delete", wantErr: ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := targetStatus(tt.current, tt.action)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("targetStatus(%q, %q) error = %v, want %v", tt.current, tt.action, err, tt.wantErr)
			}
			if tt.wantErr == nil && got != tt.want {
				t.Fatalf("targetStatus(%q, %q) = %q, want %q", tt.current, tt.action, got, tt.want)
			}
		})
	}
}

func TestAdminOnlyAction(t *testing.T) {
	for _, action := range []string{"deactivate", "reopen"} {
		if !adminOnlyAction(action) {
			t.Errorf("adminOnlyAction(%q) = false, want true", action)
		}
	}
	for _, action := range []string{"activate", "complete", "archive", "reactivate", "delete", ""} {
		if adminOnlyAction(action) {
			t.Errorf("adminOnlyAction(%q) = true, want false", action)
		}
	}
}
