package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	jwtSecret, err := common.ReadSecret("/run/secrets/jwt_key", "JWT_SECRET")
	if err != nil {
		slog.Error("failed to read jwt secret", "error", err)
		os.Exit(1)
	}
	mw.SetJWTSecret([]byte(jwtSecret))

	db, err := common.OpenDB()
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	authRepo := auth.NewRepository(db)
	mw.TokenVersionValidator = func(userID string, version int) bool {
		user, err := authRepo.GetByID(userID)
		if err != nil || user == nil {
			return false
		}
		return user.TokenVersion == version
	}
	authSvc := auth.NewService(authRepo, []byte(jwtSecret))
	authHandler := auth.NewHandler(authSvc)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(mw.RequestID)
	r.Use(mw.CORS)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})

	r.Mount("/api/v1/auth", authHandler.Routes(mw.Audit(db)))

	port := commonEnv("PORT", "8000")
	slog.Info("server starting", "port", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func commonEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
