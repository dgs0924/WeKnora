package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	users      interfaces.UserRepository
}

func NewCodexHandler(connection *service.CodexConnection, models interfaces.ModelRepository, users interfaces.UserRepository) *CodexHandler {
	return &CodexHandler{connection: connection, models: models, users: users}
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
	manage := types.IsSystemAdminFromContext(c.Request.Context())
	if h.connection.Err != nil || h.connection.Runtime == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": true, "connected": false, "owner": true, "manage_grants": manage, "connection_error": "unavailable"})
		return
	}
	status, err := h.connection.Runtime.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"enabled": true, "connected": false, "owner": true, "manage_grants": manage, "connection_error": "unavailable"})
		return
	}
	result := gin.H{"enabled": true, "connected": status.LoggedIn, "owner": true, "manage_grants": manage}
	if status.LoggedIn {
		result["email"] = status.Email
		result["plan"] = status.Plan
	}
	c.JSON(http.StatusOK, result)
}

func (h *CodexHandler) Usage(c *gin.Context) {
	if _, ok := h.owner(c); !ok {
		return
	}
	if h.connection == nil || h.connection.Runtime == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex usage is unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	usage, err := h.connection.Runtime.Usage(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex usage is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": usage.Available, "limits": usage.Limits, "updated_at": time.Now().UTC().Format(time.RFC3339)})
}

type codexGrantModelResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	UserIDs     []string `json:"user_ids"`
}

func (h *CodexHandler) grantModels(c *gin.Context, owner string) ([]codexGrantModelResponse, bool) {
	if h.models == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Codex models are unavailable"})
		return nil, false
	}
	repo, supported := h.models.(interfaces.CodexGrantRepository)
	if !supported {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Codex grant storage is unavailable"})
		return nil, false
	}
	rows, err := repo.ListCodexModelsForOwner(c.Request.Context(), owner)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex models are unavailable"})
		return nil, false
	}
	grants, err := repo.ListCodexGrants(c.Request.Context(), owner)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex grants are unavailable"})
		return nil, false
	}
	byModel := make(map[string][]string)
	for _, grant := range grants {
		byModel[grant.ModelID] = append(byModel[grant.ModelID], grant.UserID)
	}
	result := make([]codexGrantModelResponse, 0)
	for _, row := range rows {
		if row == nil {
			continue
		}
		ids := byModel[row.ID]
		if ids == nil {
			ids = []string{}
		}
		result = append(result, codexGrantModelResponse{ID: row.ID, Name: row.Name, DisplayName: row.DisplayName, UserIDs: ids})
	}
	return result, true
}

// Grants returns the owner-managed ACL projection. It intentionally exposes
// no Codex CLI identity, credentials, or model parameters.
func (h *CodexHandler) Grants(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	if !types.IsSystemAdminFromContext(c.Request.Context()) {
		c.JSON(http.StatusForbidden, gin.H{"error": "system administrator access is required"})
		return
	}
	models, ok := h.grantModels(c, owner)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// GrantUsers lists active platform users, regardless of their workspace.
func (h *CodexHandler) GrantUsers(c *gin.Context) {
	if _, ok := h.owner(c); !ok {
		return
	}
	if !types.IsSystemAdminFromContext(c.Request.Context()) {
		c.JSON(http.StatusForbidden, gin.H{"error": "system administrator access is required"})
		return
	}
	if h.users == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user directory is unavailable"})
		return
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := h.users.ListUsers(c.Request.Context(), 0, 0)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "user directory is unavailable"})
		return
	}
	active := make([]*types.User, 0, len(rows))
	for _, u := range rows {
		if u != nil && u.IsActive {
			active = append(active, u)
		}
	}
	hasMore := offset+limit < len(active)
	if offset > len(active) {
		offset = len(active)
	}
	end := offset + limit
	if end > len(active) {
		end = len(active)
	}
	active = active[offset:end]
	users := make([]gin.H, 0, len(active))
	for _, u := range active {
		users = append(users, gin.H{"id": u.ID, "username": u.Username, "email": u.Email, "avatar": u.Avatar})
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "has_more": hasMore})
}

// ReplaceUserGrants atomically replaces a single current member's selections
// while leaving every other member's grants untouched.
func (h *CodexHandler) ReplaceUserGrants(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	if !types.IsSystemAdminFromContext(c.Request.Context()) {
		c.JSON(http.StatusForbidden, gin.H{"error": "system administrator access is required"})
		return
	}
	userID := strings.TrimSpace(c.Param("userID"))
	if userID == "" || h.users == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "an active user is required"})
		return
	}
	var req struct {
		ModelIDs *[]string `json:"model_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_ids must be an array"})
		return
	}
	if req.ModelIDs == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_ids must be an array"})
		return
	}
	user, err := h.users.GetUserByID(c.Request.Context(), userID)
	if err != nil || user == nil || !user.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selected user is not active"})
		return
	}
	unique := make([]string, 0, len(*req.ModelIDs))
	seen := make(map[string]struct{}, len(*req.ModelIDs))
	for _, id := range *req.ModelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model_ids contains an invalid model"})
			return
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	repo, supported := h.models.(interfaces.CodexGrantRepository)
	if !supported {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Codex grant storage is unavailable"})
		return
	}
	if err := repo.ReplaceCodexGrantsForUser(c.Request.Context(), owner, userID, owner, unique); err != nil {
		if app, ok := apperrors.IsAppError(err); ok && app.HTTPCode < 500 {
			c.JSON(app.HTTPCode, gin.H{"error": app.Message})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "one or more selected models are unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "model_ids": unique})
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
