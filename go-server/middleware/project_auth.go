package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type projectRoleKeyType string

const projectRoleKey projectRoleKeyType = "project_role"

type ProjectMemberLookup func(projectID, userID string) (role, status string, found bool, err error)

func RequireProjectAccess(lookup ProjectMemberLookup, minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil {
				common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
				return
			}
			if claims.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}

			projectID := chi.URLParam(r, "id")
			if projectID == "" {
				common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少项目 ID", nil)
				return
			}

			role, status, found, err := lookup(projectID, claims.UserID)
			if err != nil {
				common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "项目权限查询失败", nil)
				return
			}
			if !found || status != "active" || ProjectRoleRank(role) < ProjectRoleRank(minRole) {
				common.WriteError(w, r, http.StatusForbidden, "permission_denied", "当前用户无权访问该项目", nil)
				return
			}

			ctx := context.WithValue(r.Context(), projectRoleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetProjectRole(ctx context.Context) string {
	role, _ := ctx.Value(projectRoleKey).(string)
	return role
}

func ProjectRoleRank(role string) int {
	switch role {
	case "viewer":
		return 1
	case "member":
		return 2
	case "maintainer":
		return 3
	case "owner":
		return 4
	default:
		return 0
	}
}
