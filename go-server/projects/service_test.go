package projects

import (
	"errors"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
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

// D11：项目创建限 maintainer+admin，viewer/member 在 service 层即被拒（纵深校验）。
func TestCreateProjectRoleRestriction(t *testing.T) {
	svc := NewService(nil, nil, nil)
	req := CreateProjectRequest{Code: "P1", Name: "项目一"}
	for _, role := range []string{auth.RoleViewer, auth.RoleMember} {
		if _, err := svc.Create(req, "user-1", role); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Create with role %s: got %v, want ErrForbidden", role, err)
		}
	}
}

func TestValidateDate(t *testing.T) {
	for name, tc := range map[string]struct {
		in      *string
		wantErr error
	}{
		"nil ok":     {nil, nil},
		"empty ok":   {strPtr(""), nil},
		"valid ok":   {strPtr("2026-08-01"), nil},
		"bad format": {strPtr("2026/08/01"), ErrInvalidInput},
		"not a date": {strPtr("2026-13-01"), ErrInvalidInput},
		"partial":    {strPtr("2026-08"), ErrInvalidInput},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDate(tc.in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateDate(%v) = %v, want %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestDefaultString(t *testing.T) {
	tests := []struct {
		in, def, want string
	}{
		{"", "fallback", "fallback"},
		{"   ", "fallback", "fallback"},
		{" value ", "fallback", "value"},
	}
	for _, tt := range tests {
		if got := defaultString(tt.in, tt.def); got != tt.want {
			t.Errorf("defaultString(%q, %q) = %q, want %q", tt.in, tt.def, got, tt.want)
		}
	}
}

func TestValidEnums(t *testing.T) {
	for status, want := range map[string]bool{
		StatusDraft: true, StatusActive: true, StatusCompleted: true, StatusArchived: true,
		"bogus": false, "": false,
	} {
		if got := validStatus(status); got != want {
			t.Errorf("validStatus(%q) = %v, want %v", status, got, want)
		}
	}
	for vis, want := range map[string]bool{
		VisibilityRestricted: true, VisibilityWorkspace: true,
		"public": false, "": false,
	} {
		if got := validVisibility(vis); got != want {
			t.Errorf("validVisibility(%q) = %v, want %v", vis, got, want)
		}
	}
	for policy, want := range map[string]bool{
		CommentPolicyEveryone: true, CommentPolicyMembers: true, CommentPolicyDisabled: true,
		"none": false, "": false,
	} {
		if got := validCommentPolicy(policy); got != want {
			t.Errorf("validCommentPolicy(%q) = %v, want %v", policy, got, want)
		}
	}
	for role, want := range map[string]bool{
		RoleOwner: true, RoleMaintainer: true, RoleMember: true, RoleViewer: true,
		"admin": false, "": false,
	} {
		if got := validProjectRole(role); got != want {
			t.Errorf("validProjectRole(%q) = %v, want %v", role, got, want)
		}
	}
}

func strPtr(value string) *string { return &value }
