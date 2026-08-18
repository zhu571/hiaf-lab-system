package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type adminProjectKeyType string

const adminProjectKey adminProjectKeyType = "project_admin"

type Permission string

const (
	PermRead             Permission = "read"
	PermCreateLog        Permission = "create_log"
	PermUpdateOwnLog     Permission = "update_own_log"
	PermUpdateAnyLog     Permission = "update_any_log"
	PermCreateIssue      Permission = "create_issue"
	PermUpdateIssue      Permission = "update_issue"
	PermCreateExperience Permission = "create_experience"
	PermReviewExperience Permission = "review_experience"
	PermManageExperience Permission = "manage_experience"
	PermManageMembers    Permission = "manage_members"
	PermManageProject    Permission = "manage_project"
)

var rolePermissions = map[string][]Permission{
	"viewer": {
		PermRead,
	},
	"member": {
		PermRead,
		PermCreateLog,
		PermUpdateOwnLog,
		PermCreateIssue,
		PermUpdateIssue,
		PermCreateExperience,
	},
	"maintainer": {
		PermRead,
		PermCreateLog,
		PermUpdateOwnLog,
		PermUpdateAnyLog,
		PermCreateIssue,
		PermUpdateIssue,
		PermCreateExperience,
		PermReviewExperience,
		PermManageExperience,
		PermManageMembers,
		PermManageProject,
	},
	"owner": {
		PermRead,
		PermCreateLog,
		PermUpdateOwnLog,
		PermUpdateAnyLog,
		PermCreateIssue,
		PermUpdateIssue,
		PermCreateExperience,
		PermReviewExperience,
		PermManageExperience,
		PermManageMembers,
		PermManageProject,
	},
}

func HasPermission(db *sql.DB, projectID, userID string, perm Permission) (bool, error) {
	var userRole string
	err := db.QueryRow(`SELECT role FROM users WHERE id = $1`, userID).Scan(&userRole)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if userRole == "admin" {
		return true, nil
	}

	var projectRole string
	err = db.QueryRow(
		`SELECT role
		 FROM project_members
		 WHERE project_id = $1 AND user_id = $2 AND status = 'active'`,
		projectID, userID,
	).Scan(&projectRole)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return roleHasPermission(projectRole, perm), nil
}

func RequireProjectPermission(db *sql.DB, perm Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil {
				common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
				return
			}

			projectID := chi.URLParam(r, "id")
			if projectID == "" {
				common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少项目 ID", nil)
				return
			}

			userID := EffectiveUserID(r.Context())
			if claims.Role == "admin" {
				allowed, err := HasPermission(db, projectID, userID, perm)
				if err != nil {
					common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "项目权限查询失败", nil)
					return
				}
				if !allowed {
					common.WriteError(w, r, http.StatusForbidden, "permission_denied", "当前用户无权访问该项目", nil)
					return
				}
				ctx := context.WithValue(r.Context(), adminProjectKey, true)
				ctx = context.WithValue(ctx, projectRoleKey, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			var role string
			err := db.QueryRow(
				`SELECT role
				 FROM project_members
				 WHERE project_id = $1 AND user_id = $2 AND status = 'active'`,
				projectID, userID,
			).Scan(&role)
			if err != nil {
				if err == sql.ErrNoRows {
					common.WriteError(w, r, http.StatusForbidden, "permission_denied", "当前用户无权访问该项目", nil)
					return
				}
				common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "项目权限查询失败", nil)
				return
			}
			if !roleHasPermission(role, perm) {
				common.WriteError(w, r, http.StatusForbidden, "permission_denied", "当前用户无权访问该项目", nil)
				return
			}

			ctx := context.WithValue(r.Context(), projectRoleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func IsProjectAdmin(ctx context.Context) bool {
	ok, _ := ctx.Value(adminProjectKey).(bool)
	return ok
}

// ProjectSummary 是权限过滤后的项目最小视图，用于服务端注入 AI 解析上下文。
type ProjectSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListProjectsWithPermission 返回用户拥有指定权限的 active 项目。
// admin 返回全部 active 项目；其他用户取其 active 成员项目，再按 rolePermissions 过滤。
func ListProjectsWithPermission(db *sql.DB, userID string, perm Permission) ([]ProjectSummary, error) {
	var userRole string
	err := db.QueryRow(`SELECT role FROM users WHERE id = $1`, userID).Scan(&userRole)
	if err != nil {
		if err == sql.ErrNoRows {
			return []ProjectSummary{}, nil
		}
		return nil, err
	}
	if userRole == "admin" {
		rows, err := db.Query(`SELECT id, name FROM projects WHERE status = 'active' ORDER BY name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []ProjectSummary{}
		for rows.Next() {
			var p ProjectSummary
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, rows.Err()
	}

	rows, err := db.Query(
		`SELECT p.id, p.name, pm.role
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 WHERE pm.user_id = $1 AND pm.status = 'active' AND p.status = 'active'
		 ORDER BY p.name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectSummary{}
	for rows.Next() {
		var p ProjectSummary
		var role string
		if err := rows.Scan(&p.ID, &p.Name, &role); err != nil {
			return nil, err
		}
		if roleHasPermission(role, perm) {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func roleHasPermission(role string, perm Permission) bool {
	for _, p := range rolePermissions[role] {
		if p == perm {
			return true
		}
	}
	return false
}
