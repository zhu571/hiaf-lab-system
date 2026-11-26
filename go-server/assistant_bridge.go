package main

import (
	"context"
	"database/sql"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// assistantProjectAccess 把项目成员检查注入 assistant，assistant 自身不读项目表。
type assistantProjectAccess struct {
	db *sql.DB
}

func (a assistantProjectAccess) CanReadProject(ctx context.Context, userID, projectID string) (bool, error) {
	return middleware.IsActiveProjectMember(a.db, projectID, userID)
}
