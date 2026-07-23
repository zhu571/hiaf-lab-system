package auth

import "time"

// Role values for users.
const (
	RoleAdmin      = "admin"
	RoleMaintainer = "maintainer"
	RoleMember     = "member"
	RoleViewer     = "viewer"
	RoleAgent      = "agent"
)

// User represents an account in the system.
type User struct {
	ID             string     `json:"id"`
	Username       string     `json:"username"`
	PasswordHash   string     `json:"-"`
	DisplayName    string     `json:"display_name"`
	Role           string     `json:"role"`
	Disabled       bool       `json:"disabled"`
	MustChangePW   bool       `json:"must_change_password"`
	FailedAttempts int        `json:"-"`
	TokenVersion   int        `json:"-"`
	LockedUntil    *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// UserInfo is the public profile returned by /me.
type UserInfo struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
	MustChangePW bool      `json:"must_change_password"`
}

// RegisterRequest is the body for account creation.
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginRequest is the body for authentication.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse returns tokens after successful authentication.
type LoginResponse struct {
	AccessToken        string    `json:"access_token"`
	RefreshToken       string    `json:"refresh_token"`
	ExpiresIn          int       `json:"expires_in"`
	RefreshExpiresIn   int       `json:"refresh_expires_in"`
	CSRFToken          string    `json:"csrf_token"`
	MustChangePassword bool      `json:"must_change_password"`
	User               *UserInfo `json:"user"`
}

// ChangePasswordRequest is the body for password updates.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// RefreshRequest is the body for token rotation.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type AdminUpdateUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Role        *string `json:"role,omitempty"`
	Disabled    *bool   `json:"disabled,omitempty"`
}

type AdminCreateUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role,omitempty"`
	Password    string `json:"password,omitempty"`
}

type AdminResetPasswordRequest struct {
	NewPassword string `json:"new_password,omitempty"`
}

type AdminResetPasswordResponse struct {
	TemporaryPassword string `json:"temporary_password"`
}
