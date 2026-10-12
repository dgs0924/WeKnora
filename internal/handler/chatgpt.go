package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/chatgpt"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type chatGPTAccounts interface {
	Start(context.Context, string) (chatgpt.StartResult, error)
	Status(context.Context, string) (chatgpt.Status, error)
	Models(context.Context, string) ([]chatgpt.Model, error)
	Disconnect(context.Context, string) error
}

type ChatGPTHandler struct {
	accounts chatGPTAccounts
	manager  *chatgpt.Manager
	models   interfaces.ModelRepository
}

func NewChatGPTHandler(accounts *chatgpt.Manager, models interfaces.ModelRepository) *ChatGPTHandler {
	return &ChatGPTHandler{accounts: accounts, manager: accounts, models: models}
}

func chatGPTIdentity(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	uid, err := service.ChatGPTUser(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "A personal WeKnora session is required"})
		return "", false
	}
	return uid, true
}

// Provider errors are deliberately collapsed rather than returning token
// endpoint bodies or private filesystem paths to the client.
func chatGPTError(c *gin.Context, err error) {
	if app, ok := apperrors.IsAppError(err); ok && app.HTTPCode < 500 {
		c.JSON(app.HTTPCode, gin.H{"error": app.Message})
		return
	}
	switch {
	case errors.Is(err, chatgpt.ErrNotConnected), errors.Is(err, chatgpt.ErrReauthenticationNeeded):
		c.JSON(http.StatusConflict, gin.H{"error": "Reconnect your ChatGPT account to continue", "code": "chatgpt_reconnect_required"})
	case errors.Is(err, chatgpt.ErrTokenSharingUnavailable):
		c.JSON(http.StatusForbidden, gin.H{"error": "ChatGPT plan access was not granted", "code": "chatgpt_plan_not_granted"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "ChatGPT connection is unavailable. Check the local callback and reconnect."})
	}
}

func (h *ChatGPTHandler) Status(c *gin.Context) {
	uid, ok := chatGPTIdentity(c)
	if !ok {
		return
	}
	status, err := h.accounts.Status(c.Request.Context(), uid)
	if err != nil {
		chatGPTError(c, err)
		return
	}
	result := gin.H{"connected": status.Connected, "loopback_required": true, "pending": status.Pending}
	if status.Account != nil {
		result["email"] = status.Account.EmailHint
		result["expires_at"] = status.Account.ExpiresAt
		result["plan_enabled"] = status.Account.PlanEnabled
	}
	c.JSON(http.StatusOK, result)
}
func (h *ChatGPTHandler) Authorize(c *gin.Context) {
	uid, ok := chatGPTIdentity(c)
	if !ok {
		return
	}
	result, err := h.accounts.Start(c.Request.Context(), uid)
	if err != nil {
		chatGPTError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *ChatGPTHandler) Models(c *gin.Context) {
	uid, ok := chatGPTIdentity(c)
	if !ok {
		return
	}
	models, err := h.accounts.Models(c.Request.Context(), uid)
	if err != nil {
		chatGPTError(c, err)
		return
	}
	enabled := map[string]string{}
	if tenantID, hasTenant := types.TenantIDFromContext(c.Request.Context()); hasTenant && tenantID != 0 && h.models != nil {
		rows, err := h.models.List(c.Request.Context(), tenantID, types.ModelTypeKnowledgeQA, "")
		if err != nil {
			chatGPTError(c, err)
			return
		}
		for _, row := range rows {
			if !row.IsBuiltin && row.Parameters.ExtraConfig["chatgpt_plan"] == "true" && row.Parameters.ExtraConfig["chatgpt_owner"] == uid {
				enabled[row.Name] = row.ID
			}
		}
	}
	result := make([]gin.H, 0, len(models))
	for _, model := range models {
		entry := gin.H{"slug": model.Slug, "display_name": model.DisplayName}
		if id := enabled[model.Slug]; id != "" {
			entry["id"] = id
		}
		result = append(result, entry)
	}
	c.JSON(http.StatusOK, gin.H{"models": result})
}
func (h *ChatGPTHandler) Disconnect(c *gin.Context) {
	uid, ok := chatGPTIdentity(c)
	if !ok {
		return
	}
	if err := h.accounts.Disconnect(c.Request.Context(), uid); err != nil {
		chatGPTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *ChatGPTHandler) SelectModel(c *gin.Context) {
	_, ok := chatGPTIdentity(c)
	if !ok {
		return
	}
	var req struct {
		Slug string `json:"slug" binding:"required,max=200"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Select a ChatGPT model"})
		return
	}
	model, err := service.EnableChatGPTModel(c.Request.Context(), h.manager, h.models, req.Slug)
	if err != nil {
		chatGPTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"model_id": model.ID})
}
