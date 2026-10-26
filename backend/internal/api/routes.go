package api

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

type User struct {
	ID         string      `json:"id"`
	Email      string      `json:"email"`
	Name       string      `json:"name"`
	Username   string      `json:"username"`
	AuthSource string      `json:"authSource"`
	Access     rbac.Access `json:"access"`
}

func Register(routes *router.Router[*core.RequestEvent], publicDir string) {
	routes.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "platformRequestMetadata", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority - 1,
		Func: func(e *core.RequestEvent) error {
			requestID := rand.Text()
			e.Set("requestId", requestID)
			e.Response.Header().Set("X-Request-ID", requestID)
			if !strings.HasPrefix(e.Request.URL.Path, "/api/v1/") {
				return e.Next()
			}
			e.Response.Header().Set("Cache-Control", "no-store")
			started := time.Now()
			err := e.Next()
			status := http.StatusOK
			if err != nil {
				apiError := router.ToApiError(err)
				status = apiError.Status
				code := map[int]string{408: "REQUEST_TIMEOUT", 400: "BAD_REQUEST", 401: "UNAUTHENTICATED", 403: "FORBIDDEN", 404: "NOT_FOUND", 405: "METHOD_NOT_ALLOWED", 409: "CONFLICT", 429: "RATE_LIMITED", 503: "UNAVAILABLE"}[status]
				if code == "" {
					code = "INTERNAL_ERROR"
				}
				message := apiError.Message
				if status >= 500 {
					slog.ErrorContext(e.Request.Context(), "api failure", "requestId", requestID, "error", apiError.RawData())
					message = "服务暂时不可用，请稍后重试。"
				}
				if publicCode, ok := e.Get("publicErrorCode").(string); ok {
					code = publicCode
				}
				if publicMessage, ok := e.Get("publicErrorMessage").(string); ok {
					message = publicMessage
				}
				err = e.JSON(status, map[string]any{"error": map[string]string{"code": code, "message": message}, "requestId": requestID})
			}
			actorID := ""
			if e.Auth != nil {
				actorID = e.Auth.Id
			}
			slog.InfoContext(e.Request.Context(), "api request", "actorId", actorID, "requestId", requestID, "method", e.Request.Method, "path", e.Request.URL.Path, "status", status, "durationMs", time.Since(started).Milliseconds())
			return err
		},
	})
	group := routes.Group("/api/v1")
	group.GET("/health", func(e *core.RequestEvent) error {
		if _, err := e.App.DB().NewQuery("SELECT 1").WithContext(e.Request.Context()).Execute(); err != nil {
			return e.Error(http.StatusServiceUnavailable, "", err)
		}
		return response(e, map[string]string{"status": "ok"})
	})
	group.GET("/auth/me", func(e *core.RequestEvent) error {
		access, err := rbac.Resolve(e.App, e.Auth.Id)
		if err != nil {
			return e.InternalServerError("", err)
		}
		return response(e, User{ID: e.Auth.Id, Email: e.Auth.Email(), Name: e.Auth.GetString("name"), Username: e.Auth.GetString("username"), AuthSource: e.Auth.GetString("auth_source"), Access: access})
	}).Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	registerClusterRoutes(group)
	registerConfigurationRoutes(group)
	registerSlurmRoutes(group)
	registerPartitionRoutes(group)
	registerOverviewRoutes(group)
	registerJobControlRoutes(group)
	registerNodeControlRoutes(group)
	registerAuditRoutes(group)
	// Never serve the SPA for missing API routes.
	routes.Any("/api/{path...}", func(e *core.RequestEvent) error { return e.NotFoundError("接口不存在。", nil) })
	if _, err := os.Stat(publicDir + "/index.html"); err == nil {
		static := apis.Static(os.DirFS(publicDir), true)
		routes.Any("/{path...}", func(e *core.RequestEvent) error {
			if e.Request.Method != http.MethodGet && e.Request.Method != http.MethodHead {
				return e.Error(http.StatusMethodNotAllowed, "不支持的请求方法。", nil)
			}
			return static(e)
		})
	}
}

func response(e *core.RequestEvent, data any) error {
	return e.JSON(http.StatusOK, map[string]any{"data": data, "requestId": e.Get("requestId")})
}
