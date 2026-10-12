package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// CodexHandler exposes the server's pre-authorized CLI only to the configured
// web user. It deliberately has no authorize/disconnect endpoint: those are
// deployment operations and must not be triggered from a tenant browser.
type CodexHandler struct {
	connection *service.CodexConnection
	models     interfaces.ModelRepository
}

func NewCodexHandler(connection *service.CodexConnection, models interfaces.ModelRepository) *CodexHandler {
	return &CodexHandler{connection: connection, models: models}
}

func (h *CodexHandler) owner(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	if h.connection == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Codex plan is not enabled on this server"})
		return "", false
	}
	uid, err := service.CodexUser(c.Request.Context(), h.connection.Owner)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "The configured personal WeKnora session is required"})
		return "", false
	}
	return uid, true
}

func (h *CodexHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// Do not disclose the CLI account identity to a different tenant user or
	// API-key caller. A minimal availability projection is safe for UI gating.
	if h.connection == nil || strings.TrimSpace(h.connection.Owner) == "" {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "connected": false})
		return
	}
	if _, err := service.CodexUser(c.Request.Context(), h.connection.Owner); err != nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "connected": false})
		return
	}
	if h.connection.Err != nil || h.connection.Runtime == nil {
		c.JSON(http.StatusBadGateway, gin.H{"enabled": true, "connected": false, "error": "Codex CLI configuration is unavailable"})
		return
	}
	status, err := h.connection.Runtime.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"enabled": true, "connected": false, "error": "Codex CLI status is unavailable"})
		return
	}
	result := gin.H{"enabled": true, "connected": status.LoggedIn}
	if status.LoggedIn {
		result["email"] = status.Email
		result["plan"] = status.Plan
	}
	c.JSON(http.StatusOK, result)
}

func (h *CodexHandler) Models(c *gin.Context) {
	uid, ok := h.owner(c)
	if !ok {
		return
	}
	if modelType := strings.TrimSpace(c.Query("model_type")); modelType != "" && modelType != string(types.ModelTypeKnowledgeQA) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Codex plan supports only KnowledgeQA models"})
		return
	}
	if h.connection.Runtime == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Codex CLI configuration is unavailable"})
		return
	}
	status, err := h.connection.Runtime.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex CLI status is unavailable"})
		return
	}
	if !status.LoggedIn {
		c.JSON(http.StatusConflict, gin.H{"error": "Codex CLI is not signed in"})
		return
	}
	available, err := h.connection.Runtime.Models(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex model catalog is unavailable"})
		return
	}
	enabled := map[string]string{}
	if tenantID, present := types.TenantIDFromContext(c.Request.Context()); present && tenantID != 0 && h.models != nil {
		rows, err := h.models.List(c.Request.Context(), tenantID, types.ModelTypeKnowledgeQA, "")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Codex models are unavailable"})
			return
		}
		for _, row := range rows {
			if !row.IsBuiltin && row.Parameters.ExtraConfig["codex_plan"] == "true" && row.Parameters.ExtraConfig["codex_owner"] == uid {
				enabled[row.Name] = row.ID
			}
		}
	}
	result := make([]gin.H, 0, len(available))
	for _, model := range available {
		slug := model.ID
		if slug == "" {
			slug = model.Name
		}
		entry := gin.H{"slug": slug, "display_name": model.DisplayName}
		if entry["display_name"] == "" {
			entry["display_name"] = model.Name
		}
		if id := enabled[model.Name]; id != "" {
			entry["id"] = id
		}
		result = append(result, entry)
	}
	c.JSON(http.StatusOK, gin.H{"models": result})
}

func (h *CodexHandler) SelectModel(c *gin.Context) {
	if _, ok := h.owner(c); !ok {
		return
	}
	var req struct {
		Slug string `json:"slug" binding:"required,max=200"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Select a Codex model"})
		return
	}
	model, err := service.EnableCodexModel(c.Request.Context(), h.connection, h.models, req.Slug)
	if err != nil {
		if app, ok := apperrors.IsAppError(err); ok && app.HTTPCode < 500 {
			c.JSON(app.HTTPCode, gin.H{"error": app.Message})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex model is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"model_id": model.ID})
}
