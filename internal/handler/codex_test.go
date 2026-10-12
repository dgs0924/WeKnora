package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexStatusHidesServerConnectionFromOtherUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "alice"}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "bob")
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "bob"})
		c.Request = c.Request.WithContext(ctx)
	})
	r.GET("/status", h.Status)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	require.NotContains(t, w.Body.String(), "email")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestCodexOwnerSeesConfigurationFailureWithoutAccountDisclosure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "alice"}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "alice")
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
		c.Request = c.Request.WithContext(ctx)
	})
	r.GET("/status", h.Status)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "configuration is unavailable")
	require.NotContains(t, w.Body.String(), "email")
}
