package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterCodexRoutes exposes read/select operations only. Server-side Codex
// login is intentionally outside HTTP and belongs to deployment administration.
func RegisterCodexRoutes(r *gin.RouterGroup, h *handler.CodexHandler) {
	if h == nil {
		return
	}
	g := r.Group("/codex")
	g.GET("/status", h.Status)
	g.GET("/models", h.Models)
	g.POST("/models/select", h.SelectModel)
}
