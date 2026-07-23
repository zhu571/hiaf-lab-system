package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"golang.org/x/crypto/argon2"
)

// Service errors returned by the auth domain.
var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrAccountLocked      = errors.New("账户已锁定，请15分钟后再试")
	ErrAccountDisabled    = errors.New("账户已停用，请联系管理员")
	ErrUsernameTaken      = errors.New("用户名已存在")
	ErrPasswordTooShort   = errors.New("密码长度至少8位")
	ErrInvalidRole        = errors.New("用户角色无效")
	ErrCannotModifySelf   = errors.New("不能通过用户管理修改自己的账户")
	ErrLastActiveAdmin    = errors.New("不能停用或降级最后一个管理员账户")
)

const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 2
	argon2KeyLen  = 32
	saltLen       = 16

	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

// Service encapsulates auth business logic.
type Service struct {
	repo   *Repository
	jwtKey []byte
}

// NewService creates an auth service.
func NewService(repo *Repository, jwtKey []byte) *Service {
	return &Service{repo: repo, jwtKey: jwtKey}
}

// GetUser returns a user by ID.
func (s *Service) GetUser(userID string) (*User, error) {
	return s.repo.GetByID(userID)
}

func (s *Service) ListUsers() ([]UserInfo, error) {
	users, err := s.repo.ListUsers()
	if err != nil {
		return nil, err
	}
	infos := make([]UserInfo, 0, len(users))
	for i := range users {
		infos = append(infos, toUserInfo(&users[i]))
	}
	return infos, nil
}

func (s *Service) AdminCreateUser(req AdminCreateUserRequest) (*AdminResetPasswordResponse, *UserInfo, error) {
	role := req.Role
	if role == "" {
		role = RoleMember
	}
	if !validRole(role) {
		return nil, nil, ErrInvalidRole
	}
	password := req.Password
	if password == "" {
		generated, err := generateTemporaryPassword()
		if err != nil {
			return nil, nil, err
		}
		password = generated
	}
	if len(password) < 8 {
		return nil, nil, ErrPasswordTooShort
	}
	taken, err := s.repo.IsUsernameTaken(req.Username)
	if err != nil {
		return nil, nil, err
	}
	if taken {
		return nil, nil, ErrUsernameTaken
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash admin password: %w", err)
	}
	user, err := s.repo.CreateUserWithProfile(req.Username, hash, req.DisplayName, role)
	if err != nil {
		return nil, nil, err
	}
	info := toUserInfo(user)
	return &AdminResetPasswordResponse{TemporaryPassword: password}, &info, nil
}

func (s *Service) AdminUpdateUser(actingUserID, id string, req AdminUpdateUserRequest) (*UserInfo, error) {
	if actingUserID == id {
		return nil, ErrCannotModifySelf
	}
	if req.Role != nil && !validRole(*req.Role) {
		return nil, ErrInvalidRole
	}
	if removesActiveAdmin(req) {
		target, err := s.repo.GetByID(id)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, ErrInvalidCredentials
		}
		if target.Role == RoleAdmin && !target.Disabled {
			count, err := s.repo.CountActiveAdmins()
			if err != nil {
				return nil, err
			}
			if count <= 1 {
				return nil, ErrLastActiveAdmin
			}
		}
	}
	user, err := s.repo.UpdateUser(id, req.DisplayName, req.Role, req.Disabled)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	info := toUserInfo(user)
	return &info, nil
}

// removesActiveAdmin reports whether the update would strip the target account
// of its active-admin status, either by disabling it or demoting its role.
func removesActiveAdmin(req AdminUpdateUserRequest) bool {
	if req.Disabled != nil && *req.Disabled {
		return true
	}
	return req.Role != nil && *req.Role != RoleAdmin
}

func (s *Service) AdminResetPassword(id, password string) (*AdminResetPasswordResponse, error) {
	if password == "" {
		generated, err := generateTemporaryPassword()
		if err != nil {
			return nil, err
		}
		password = generated
	}
	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}
	user, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash reset password: %w", err)
	}
	if err := s.repo.UpdatePassword(id, hash); err != nil {
		return nil, err
	}
	return &AdminResetPasswordResponse{TemporaryPassword: password}, nil
}

// Register creates a new user account.
func (s *Service) Register(username, password string) (*User, error) {
	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}

	taken, err := s.repo.IsUsernameTaken(username)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrUsernameTaken
	}

	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	return s.repo.CreateUser(username, hash)
}

// Login authenticates a user and returns token pair.
func (s *Service) Login(username, password string) (*LoginResponse, error) {
	user, err := s.repo.GetByUsername(username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		// Use constant timing to avoid user enumeration.
		_, _ = hashPassword(password)
		_, _, _ = s.repo.IncrementFailedAttempts(username)
		return nil, ErrInvalidCredentials
	}

	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		return nil, ErrAccountLocked
	}

	if user.Disabled {
		return nil, ErrAccountDisabled
	}

	if !verifyPassword(user.PasswordHash, password) {
		attempts, lockedUntil, err := s.repo.IncrementFailedAttempts(username)
		if err != nil {
			return nil, err
		}
		if lockedUntil != nil && time.Now().Before(*lockedUntil) {
			return nil, ErrAccountLocked
		}
		_ = attempts
		return nil, ErrInvalidCredentials
	}

	if err := s.repo.ResetFailedAttempts(username); err != nil {
		return nil, err
	}

	accessToken, err := middleware.GenerateToken(user.ID, user.Username, user.Role, user.TokenVersion, s.jwtKey)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, family, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	if err := s.repo.StoreRefreshToken(user.ID, refreshToken, family); err != nil {
		return nil, err
	}

	return loginResponse(user, accessToken, refreshToken), nil
}

// ChangePassword updates the password for an authenticated user.
func (s *Service) ChangePassword(userID, oldPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrPasswordTooShort
	}

	user, err := s.repo.GetByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrInvalidCredentials
	}

	if !verifyPassword(user.PasswordHash, oldPassword) {
		return ErrInvalidCredentials
	}

	newHash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	return s.repo.UpdatePassword(userID, newHash)
}

// RefreshAccessToken rotates a refresh token and issues a new access token.
func (s *Service) RefreshAccessToken(rawToken string) (*LoginResponse, error) {
	rec, err := s.repo.FindRefreshToken(rawToken)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrInvalidCredentials
	}

	if err := s.repo.RevokeRefreshToken(rec.ID); err != nil {
		return nil, err
	}

	user, err := s.repo.GetByID(rec.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if user.Disabled {
		return nil, ErrAccountDisabled
	}

	accessToken, err := middleware.GenerateToken(user.ID, user.Username, user.Role, user.TokenVersion, s.jwtKey)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	newRefreshToken, _, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	if err := s.repo.StoreRefreshToken(user.ID, newRefreshToken, rec.Family); err != nil {
		return nil, err
	}

	return loginResponse(user, accessToken, newRefreshToken), nil
}

func (s *Service) Logout(rawToken string) error {
	if rawToken == "" {
		return nil
	}
	rec, err := s.repo.FindRefreshToken(rawToken)
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	return s.repo.RevokeRefreshToken(rec.ID)
}

func loginResponse(user *User, accessToken, refreshToken string) *LoginResponse {
	info := toUserInfo(user)
	return &LoginResponse{
		AccessToken:        accessToken,
		RefreshToken:       refreshToken,
		ExpiresIn:          int(accessTokenTTL.Seconds()),
		RefreshExpiresIn:   int(refreshTokenTTL.Seconds()),
		MustChangePassword: user.MustChangePW,
		User:               &info,
	}
}

// hashPassword returns a salt:hash hex string using Argon2id.
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash), nil
}

// verifyPassword checks a password against a stored salt:hash string.
func verifyPassword(stored, password string) bool {
	salt, expectedHash, ok := splitStored(stored)
	if !ok {
		return false
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return subtle.ConstantTimeCompare(hash, expectedHash) == 1
}

// splitStored parses a stored password hash into salt and hash bytes.
func splitStored(stored string) ([]byte, []byte, bool) {
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return nil, nil, false
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil || len(salt) != saltLen {
		return nil, nil, false
	}

	hash, err := hex.DecodeString(parts[1])
	if err != nil || len(hash) != argon2KeyLen {
		return nil, nil, false
	}

	return salt, hash, true
}

// generateRefreshToken creates a new random refresh token and family identifier.
func generateRefreshToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	family := uuid.New().String()
	return hex.EncodeToString(raw), family, nil
}

func generateTemporaryPassword() (string, error) {
	raw := make([]byte, 9)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate temporary password: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func validRole(role string) bool {
	switch role {
	case RoleAdmin, RoleMaintainer, RoleMember, RoleViewer, RoleAgent:
		return true
	default:
		return false
	}
}
