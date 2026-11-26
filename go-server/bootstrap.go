package main

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/internal/config"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// bootstrap 读取 JWT/ServiceToken、初始化 middleware 全局状态并打开数据库。
// 返回的 *sql.DB 由调用方负责 Close。
func bootstrap(cfg *config.Config) (*sql.DB, string, string) {
	jwtSecret, err := common.ReadSecret(cfg.Auth.JWTSecretFile, cfg.Auth.JWTSecretEnv)
	if err != nil {
		slog.Error("failed to read jwt secret", "error", err)
		os.Exit(1)
	}
	middleware.SetJWTSecret([]byte(jwtSecret))

	serviceToken, err := common.ReadSecret(cfg.Auth.ServiceTokenFile, cfg.Auth.ServiceTokenEnv)
	if err != nil {
		slog.Warn("service token 未配置（todos scheduler 将无法拉取日报）", "error", err)
	}
	middleware.SetServiceToken(serviceToken)
	middleware.SetIdempotencyV2(cfg.Features.IdempotencyV2)

	db, err := common.OpenDBWithOptions(common.DBOptions{
		Host:         cfg.DB.Host,
		Port:         cfg.DB.Port,
		User:         cfg.DB.User,
		Name:         cfg.DB.Name,
		PasswordFile: cfg.DB.PasswordFile,
		PasswordEnv:  cfg.DB.PasswordEnv,
		MaxOpen:      cfg.DB.MaxOpen,
		MaxIdle:      cfg.DB.MaxIdle,
	})
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	return db, jwtSecret, cfg.HTTP.Port
}
