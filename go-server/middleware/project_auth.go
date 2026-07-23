package middleware

import (
	"context"
)

type projectRoleKeyType string

const projectRoleKey projectRoleKeyType = "project_role"

func GetProjectRole(ctx context.Context) string {
	role, _ := ctx.Value(projectRoleKey).(string)
	return role
}
