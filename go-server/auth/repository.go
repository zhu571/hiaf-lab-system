package auth

import (
	"database/sql"
	"fmt"
	"time"
)

// Repository provides data access for the auth module.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new auth repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// CreateUser inserts a new user and returns the persisted record.
func (r *Repository) CreateUser(username, passwordHash string) (*User, error) {
	return r.CreateUserWithProfile(username, passwordHash, "", RoleMember)
}

func (r *Repository) CreateUserWithProfile(username, passwordHash, displayName, role string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime

	err := r.db.QueryRow(
		`INSERT INTO users (username, password_hash, display_name, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, username, password_hash, display_name, role, must_change_pw,
		           failed_attempts, token_version, locked_until, created_at, updated_at, disabled`,
		username, passwordHash, displayName, role,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	return &user, nil
}

// GetByUsername fetches a user by username.
func (r *Repository) GetByUsername(username string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, display_name, role, must_change_pw,
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled
		 FROM users
		 WHERE username = $1`,
		username,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	return &user, nil
}

// GetByID fetches a user by ID.
func (r *Repository) GetByID(id string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, display_name, role, must_change_pw,
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled
		 FROM users
		 WHERE id = $1`,
		id,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	return &user, nil
}

func (r *Repository) ListUsers() ([]User, error) {
	rows, err := r.db.Query(
		`SELECT id, username, password_hash, display_name, role, must_change_pw,
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled
		 FROM users
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		var lockedUntil sql.NullTime
		if err := rows.Scan(
			&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
			&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		if lockedUntil.Valid {
			user.LockedUntil = &lockedUntil.Time
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

// UpdateUser applies the given admin edits. When the account is disabled, all
// existing refresh tokens are revoked so the session cannot be renewed.
func (r *Repository) UpdateUser(id string, displayName *string, role *string, disabled *bool) (*User, error) {
	var user User
	var lockedUntil sql.NullTime
	err := r.db.QueryRow(
		`UPDATE users
		 SET display_name = COALESCE($2, display_name),
		     role = COALESCE($3, role),
		     disabled = COALESCE($4, disabled),
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, username, password_hash, display_name, role, must_change_pw,
		           failed_attempts, token_version, locked_until, created_at, updated_at, disabled`,
		id,
		nullStringPtr(displayName),
		nullStringPtr(role),
		nullBoolPtr(disabled),
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	if disabled != nil && *disabled {
		if err := r.RevokeUserRefreshTokens(id); err != nil {
			return nil, err
		}
	}
	return &user, nil
}

// CountActiveAdmins reports how many enabled admin accounts exist.
func (r *Repository) CountActiveAdmins() (int, error) {
	var count int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM users WHERE role = $1 AND disabled = FALSE`,
		RoleAdmin,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}
	return count, nil
}

// UpdatePassword changes the password hash, increments the token version, and
// revokes all existing refresh tokens so that old access tokens are rejected.
func (r *Repository) UpdatePassword(userID, passwordHash string) error {
	_, err := r.db.Exec(
		`UPDATE users
		 SET password_hash = $2, must_change_pw = FALSE, token_version = token_version + 1, updated_at = now()
		 WHERE id = $1`,
		userID, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := r.RevokeUserRefreshTokens(userID); err != nil {
		return err
	}
	return nil
}

// RevokeUserRefreshTokens marks all refresh tokens for a user as revoked.
func (r *Repository) RevokeUserRefreshTokens(userID string) error {
	_, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}

// IncrementFailedAttempts increments the failed login counter and locks the account
// after five consecutive failures for 15 minutes.
func (r *Repository) IncrementFailedAttempts(username string) (int, *time.Time, error) {
	var attempts int
	var lockedUntil sql.NullTime

	err := r.db.QueryRow(
		`UPDATE users
		 SET failed_attempts = failed_attempts + 1,
		     locked_until = CASE
		         WHEN failed_attempts + 1 >= 5 THEN now() + interval '15 minutes'
		         ELSE locked_until
		     END
		 WHERE username = $1
		 RETURNING failed_attempts, locked_until`,
		username,
	).Scan(&attempts, &lockedUntil)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("increment failed attempts: %w", err)
	}
	var until *time.Time
	if lockedUntil.Valid {
		until = &lockedUntil.Time
	}
	return attempts, until, nil
}

// ResetFailedAttempts clears the failed login counter and lock state.
func (r *Repository) ResetFailedAttempts(username string) error {
	_, err := r.db.Exec(
		`UPDATE users
		 SET failed_attempts = 0, locked_until = NULL
		 WHERE username = $1`,
		username,
	)
	if err != nil {
		return fmt.Errorf("reset failed attempts: %w", err)
	}
	return nil
}

// IsUsernameTaken reports whether the username already exists.
func (r *Repository) IsUsernameTaken(username string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`,
		username,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check username: %w", err)
	}
	return exists, nil
}

// RefreshTokenRecord is a persisted refresh token row.
type RefreshTokenRecord struct {
	ID        string
	UserID    string
	Family    string
	ExpiresAt time.Time
	Revoked   bool
}

// StoreRefreshToken saves a bcrypt hash of the raw token.
func (r *Repository) StoreRefreshToken(userID, rawToken, family string) error {
	_, err := r.db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, family, expires_at)
		 VALUES ($1, crypt($2, gen_salt('bf')), $3, now() + interval '30 days')`,
		userID, rawToken, family,
	)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	return nil
}

// FindRefreshToken looks up a raw token by comparing it with stored bcrypt hashes.
func (r *Repository) FindRefreshToken(rawToken string) (*RefreshTokenRecord, error) {
	var rec RefreshTokenRecord
	err := r.db.QueryRow(
		`SELECT id, user_id, family, expires_at, revoked
		 FROM refresh_tokens
		 WHERE expires_at > now()
		   AND revoked = FALSE
		   AND crypt($1, token_hash) = token_hash`,
		rawToken,
	).Scan(&rec.ID, &rec.UserID, &rec.Family, &rec.ExpiresAt, &rec.Revoked)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find refresh token: %w", err)
	}
	return &rec, nil
}

// RevokeRefreshToken marks a refresh token as revoked.
func (r *Repository) RevokeRefreshToken(id string) error {
	_, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked = TRUE WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullBoolPtr(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{Valid: false}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}
