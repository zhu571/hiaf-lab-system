// +build ignore
// Minimal server to test instruments/sensors without PostgreSQL.
// Usage: go run devserver.go
package main

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/sensors"
)

func main() {
	is := instruments.NewHandler(instruments.NewService())
	ss := sensors.NewHandler(sensors.NewService())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/api/v1/instruments/piezo/status", is.PiezoStatus)
	r.Post("/api/v1/instruments/piezo/start", is.PiezoStart)
	r.Post("/api/v1/instruments/piezo/stop", is.PiezoStop)
	r.Post("/api/v1/instruments/piezo/setpoint", is.PiezoSetpoint)
	r.Get("/api/v1/sensors/latest", ss.Latest)
	r.Get("/api/v1/sensors/history", ss.History)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	http.ListenAndServe(":"+port, r)
}
