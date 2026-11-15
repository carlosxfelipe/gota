package system

import (
	"net/http"
	"runtime"
	"time"

	"github.com/carlosxfelipe/gota/repr"
)

type HealthStatus struct {
	Status        string `json:"status" doc:"Service health state" example:"ok"`
	UptimeSeconds int64  `json:"uptimeSeconds" doc:"Process uptime in seconds" example:"42"`
	GoVersion     string `json:"goVersion" doc:"Go runtime version" example:"go1.26.1"`
}

// RegisterRoutes registers non-REPR system routes that still appear in OpenAPI, Scalar, and the TypeScript SDK.
func RegisterRoutes(app *repr.App) {
	startedAt := time.Now()

	app.Get("/health", func(ctx *repr.Context) error {
		return ctx.JSON(HealthStatus{
			Status:        "ok",
			UptimeSeconds: int64(time.Since(startedAt).Seconds()),
			GoVersion:     runtime.Version(),
		})
	}).
		Summary("Service health check").
		Description("Returns service liveness and runtime uptime using an imperative non-REPR Context handler.").
		Tags("System").
		Response(http.StatusOK, HealthStatus{}, "Service health status")

	app.HandleHTTP(http.MethodGet, "/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})).
		Summary("Ping endpoint").
		Description("Standard net/http handler exposed in OpenAPI and Scalar without REPR.").
		Tags("System").
		ResponseMedia(http.StatusOK, "text/plain", nil, "Plain-text pong response")
}
