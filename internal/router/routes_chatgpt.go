package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// Personal connections are deliberately not declared as API-key endpoints.
func RegisterChatGPTRoutes(r *gin.RouterGroup, h *handler.ChatGPTHandler) {
	if h == nil {
		return
	}
	g := r.Group("/chatgpt")
	g.GET("/status", h.Status)
	g.POST("/authorize", h.Authorize)
	g.GET("/models", h.Models)
	g.POST("/models/select", h.SelectModel)
	g.DELETE("/connection", h.Disconnect)
}
