package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lib/pq"
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
		           failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language`,
		username, passwordHash, displayName, role,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	return &user, nil
}

func (r *Repository) CreateUserWithInvitation(username, passwordHash, code string) (*User, error) {
	s := sha256.Sum256([]byte(code))
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	var expires time.Time
	var used, revoked sql.NullTime
	err = tx.QueryRow(`SELECT id,expires_at,used_at,revoked_at FROM invitation_codes WHERE code_hash=$1 FOR UPDATE`, hex.EncodeToString(s[:])).Scan(&id, &expires, &used, &revoked)
	if err == sql.ErrNoRows || (err == nil && (used.Valid || revoked.Valid || !expires.After(time.Now()))) {
		return nil, ErrInvalidInvitation
	}
	if err != nil {
		return nil, fmt.Errorf("lock invitation: %w", err)
	}
	u, err := insertUser(tx, username, passwordHash, "", RoleMember)
	if err != nil {
		var pe *pq.Error
		if errors.As(err, &pe) && pe.Code == "23505" {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE invitation_codes SET used_at=now(),used_by=$2,updated_at=now() WHERE id=$1`, id, u.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}
func insertUser(tx *sql.Tx, username, passwordHash, displayName, role string) (*User, error) {
	var u User
	var locked sql.NullTime
	err := tx.QueryRow(`INSERT INTO users(username,password_hash,display_name,role) VALUES($1,$2,$3,$4) RETURNING id,username,password_hash,display_name,role,must_change_pw,failed_attempts,token_version,locked_until,created_at,updated_at,disabled,language`, username, passwordHash, displayName, role).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.MustChangePW, &u.FailedAttempts, &u.TokenVersion, &locked, &u.CreatedAt, &u.UpdatedAt, &u.Disabled, &u.Language)
	if locked.Valid {
		u.LockedUntil = &locked.Time
	}
	return &u, err
}
func (r *Repository) CreateInvitation(adminID, hash, prefix string, expires time.Time) (*InvitationCode, error) {
	var i InvitationCode
	err := r.db.QueryRow(`INSERT INTO invitation_codes(code_hash,code_prefix,created_by,expires_at) VALUES($1,$2,$3,$4) RETURNING id,code_prefix,created_by,expires_at,created_at`, hash, prefix, adminID, expires).Scan(&i.ID, &i.CodePrefix, &i.CreatedBy, &i.ExpiresAt, &i.CreatedAt)
	if err != nil {
		return nil, err
	}
	i.Status = "active"
	return &i, nil
}
func (r *Repository) ListInvitations(page, perPage int, status string) (*InvitationCodeList, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = " WHERE CASE WHEN used_at IS NOT NULL THEN 'used' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at <= now() THEN 'expired' ELSE 'active' END = $1"
		args = append(args, status)
	}
	var total int
	if err := r.db.QueryRow("SELECT count(*) FROM invitation_codes"+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.Query(`SELECT id,code_prefix,created_by,used_by,expires_at,used_at,revoked_at,created_at,CASE WHEN used_at IS NOT NULL THEN 'used' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at <= now() THEN 'expired' ELSE 'active' END FROM invitation_codes`+where+` ORDER BY created_at DESC LIMIT $`+fmt.Sprint(limitPos)+` OFFSET $`+fmt.Sprint(offsetPos), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InvitationCode{}
	for rows.Next() {
		var i InvitationCode
		var used sql.NullString
		if err := rows.Scan(&i.ID, &i.CodePrefix, &i.CreatedBy, &used, &i.ExpiresAt, &i.UsedAt, &i.RevokedAt, &i.CreatedAt, &i.Status); err != nil {
			return nil, err
		}
		if used.Valid {
			i.UsedBy = &used.String
		}
		items = append(items, i)
	}
	return &InvitationCodeList{Items: items, Total: total, Page: page, PerPage: perPage}, rows.Err()
}
func (r *Repository) RevokeInvitation(id, adminID string) (*InvitationCode, error) {
	var i InvitationCode
	var used sql.NullString
	err := r.db.QueryRow(`UPDATE invitation_codes SET revoked_at=now(),revoked_by=$2,updated_at=now() WHERE id=$1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now() RETURNING id,code_prefix,created_by,used_by,expires_at,used_at,revoked_at,created_at`, id, adminID).Scan(&i.ID, &i.CodePrefix, &i.CreatedBy, &used, &i.ExpiresAt, &i.UsedAt, &i.RevokedAt, &i.CreatedAt)
	if err == sql.ErrNoRows {
		var exists bool
		if e := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM invitation_codes WHERE id=$1)`, id).Scan(&exists); e != nil {
			return nil, e
		}
		if !exists {
			return nil, ErrInvitationNotFound
		}
		return nil, ErrInvitationNotActive
	}
	if err != nil {
		return nil, err
	}
	if used.Valid {
		i.UsedBy = &used.String
	}
	i.Status = "revoked"
	return &i, nil
}

// GetByUsername fetches a user by username.
func (r *Repository) GetByUsername(username string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime
	var lastLoginIP sql.NullString
	var lastLoginAt sql.NullTime

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, display_name, role, must_change_pw,
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language,
		        last_login_ip, last_login_at
		 FROM users
		 WHERE username = $1`,
		username,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
		&lastLoginIP, &lastLoginAt,
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
	user.LastLoginIP = lastLoginIP.String
	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}
	return &user, nil
}

// GetByID fetches a user by ID.
func (r *Repository) GetByID(id string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, display_name, role, must_change_pw,
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language
		 FROM users
		 WHERE id = $1`,
		id,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
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
		        failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language
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
			&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
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
// existing refresh tokens are revoked so the session cannot be renewed. When
// the role changes, token_version is incremented so outstanding access tokens
// (whose claims carry the old role) are rejected immediately instead of
// surviving until natural expiry.
func (r *Repository) UpdateUser(id string, displayName *string, role *string, disabled *bool) (*User, error) {
	var user User
	var lockedUntil sql.NullTime
	err := r.db.QueryRow(
		`UPDATE users
		 SET display_name = COALESCE($2, display_name),
		     role = COALESCE($3, role),
		     token_version = CASE WHEN $3 IS NOT NULL AND role IS DISTINCT FROM $3
		                          THEN token_version + 1 ELSE token_version END,
		     disabled = COALESCE($4, disabled),
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, username, password_hash, display_name, role, must_change_pw,
		           failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language`,
		id,
		nullStringPtr(displayName),
		nullStringPtr(role),
		nullBoolPtr(disabled),
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
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

// UpdateLanguage sets the user's UI language preference.
func (r *Repository) UpdateLanguage(id, language string) (*User, error) {
	var user User
	var lockedUntil sql.NullTime
	err := r.db.QueryRow(
		`UPDATE users
		 SET language = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING id, username, password_hash, display_name, role, must_change_pw,
		           failed_attempts, token_version, locked_until, created_at, updated_at, disabled, language`,
		id, language,
	).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role,
		&user.MustChangePW, &user.FailedAttempts, &user.TokenVersion, &lockedUntil, &user.CreatedAt, &user.UpdatedAt, &user.Disabled, &user.Language,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user language: %w", err)
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
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

// RevokeUserRefreshTokens 批量撤销用户的全部 refresh token：直接物理删除主表行。
// 不写黑名单：改密/停用等批量场景下旧 token 持有者即账号本人（或被停用的账号），
// 且 token_version 提升/账号停用已使其失效，不属于复用重放攻击面；
// 重放检测（FindRevokedRefreshToken）聚焦单 token 轮换/登出撤销路径。
func (r *Repository) RevokeUserRefreshTokens(userID string) error {
	_, err := r.db.Exec(
		`DELETE FROM refresh_tokens WHERE user_id = $1`,
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

// UpdateLastLogin records the source address and time of a successful login.
func (r *Repository) UpdateLastLogin(userID, ip string) error {
	_, err := r.db.Exec(
		`UPDATE users SET last_login_ip = $2, last_login_at = now() WHERE id = $1`,
		userID, ip,
	)
	return err
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

// RotateRefreshToken revokes the old token and stores its replacement atomically.
func (r *Repository) RotateRefreshToken(id, rawToken, userID string, expiresAt time.Time, newRawToken, family string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("rotate refresh token: begin tx: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM refresh_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("rotate refresh token: delete: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("rotate refresh token: check delete: %w", err)
	} else if n != 1 {
		return errors.New("refresh token not found")
	}
	if _, err := tx.Exec(
		`INSERT INTO revoked_tokens (token_lookup, user_id, expires_at)
		 VALUES (encode(digest($1, 'sha256'), 'hex'), $2, $3)
		 ON CONFLICT (token_lookup) DO NOTHING`,
		rawToken, userID, expiresAt,
	); err != nil {
		return fmt.Errorf("rotate refresh token: blacklist: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, family, expires_at)
		 VALUES ($1, crypt($2, gen_salt('bf')), $3, now() + interval '30 days')`,
		userID, newRawToken, family,
	); err != nil {
		return fmt.Errorf("rotate refresh token: store: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rotate refresh token: commit: %w", err)
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

// RevokeRefreshToken 撤销一个 refresh token：事务内物理删除主表行，并把
// token_lookup（sha256 摘要）写入 revoked_tokens 黑名单（唯一索引，O(1) 命中）。
// 替代原 UPDATE revoked=TRUE 的 bcrypt 全表扫描方案，根治 revoked 行堆积导致的
// refresh 401 慢查询；黑名单行保留到 token 原过期时间 expiresAt。
func (r *Repository) RevokeRefreshToken(id, rawToken, userID string, expiresAt time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("revoke refresh token: begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`DELETE FROM refresh_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke refresh token: delete: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("revoke refresh token: check delete: %w", err)
	} else if n != 1 {
		return errors.New("refresh token not found")
	}
	if _, err := tx.Exec(
		`INSERT INTO revoked_tokens (token_lookup, user_id, expires_at)
		 VALUES (encode(digest($1, 'sha256'), 'hex'), $2, $3)
		 ON CONFLICT (token_lookup) DO NOTHING`,
		rawToken, userID, expiresAt,
	); err != nil {
		return fmt.Errorf("revoke refresh token: blacklist: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("revoke refresh token: commit: %w", err)
	}
	return nil
}

// FindRevokedRefreshToken 查 revoked_tokens 黑名单：token_lookup 为 sha256(rawToken)
// 摘要，唯一索引 O(1) 命中（不再 bcrypt 全表扫描），只匹配未过期行：用于检测真复用
// 重放（已撤销 token 被再次使用；FindRefreshToken 只查未撤销行，检测不到）。
func (r *Repository) FindRevokedRefreshToken(rawToken string) (*RefreshTokenRecord, error) {
	var rec RefreshTokenRecord
	err := r.db.QueryRow(
		`SELECT user_id, expires_at
		 FROM revoked_tokens
		 WHERE token_lookup = encode(digest($1, 'sha256'), 'hex')
		   AND expires_at > now()`,
		rawToken,
	).Scan(&rec.UserID, &rec.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find revoked refresh token: %w", err)
	}
	return &rec, nil
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
