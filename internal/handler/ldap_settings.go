package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	ldapauth "github.com/Tencent/WeKnora/internal/auth"
	"github.com/Tencent/WeKnora/internal/config"
)

type ldapConfigUpdateRequest struct {
	Enabled           bool   `json:"enabled"`
	URL               string `json:"url"`
	BindDN            string `json:"bind_dn"`
	BindPassword      string `json:"bind_password"`
	BaseDN            string `json:"base_dn"`
	UserFilter        string `json:"user_filter"`
	UsernameAttribute string `json:"username_attribute"`
	EmailAttribute    string `json:"email_attribute"`
	AutoProvision     bool   `json:"auto_provision"`
	StartTLS          bool   `json:"start_tls"`
	AllowPlaintext    bool   `json:"allow_plaintext"`
	TLSServerName     string `json:"tls_server_name"`
	TLSCAPEM          string `json:"tls_ca_pem"`
}

func ldapConfigResponse(cfg *config.LDAPAuthConfig) gin.H {
	return gin.H{
		"enabled": cfg.Enable, "url": cfg.URL, "bind_dn": cfg.BindDN,
		"base_dn": cfg.BaseDN, "user_filter": cfg.UserFilter,
		"username_attribute": cfg.UsernameAttribute, "email_attribute": cfg.EmailAttribute,
		"auto_provision": cfg.AutoProvision, "start_tls": cfg.StartTLS, "allow_plaintext": cfg.AllowPlaintext,
		"tls_server_name": cfg.TLSServerName, "tls_ca_pem": cfg.TLSCAPEM,
		"has_bind_password": cfg.BindPassword != "",
	}
}

func (h *SystemHandler) GetLDAPConfig(c *gin.Context) {
	cfg, err := service.ResolveLDAPConfig(c.Request.Context(), h.cfg, h.systemSettingSvc)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LDAP configuration cannot be read"})
		return
	}
	c.JSON(http.StatusOK, ldapConfigResponse(cfg))
}

func (h *SystemHandler) UpdateLDAPConfig(c *gin.Context) {
	var req ldapConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LDAP configuration"})
		return
	}
	current, err := service.ResolveLDAPConfig(c.Request.Context(), h.cfg, h.systemSettingSvc)
	if err != nil && req.BindPassword == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Existing LDAP secret cannot be read; enter a new password"})
		return
	}
	cfg := ldapConfigFromRequest(req, current)
	if err := service.SaveLDAPConfig(c.Request.Context(), h.systemSettingSvc, cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ldapConfigResponse(cfg))
}

func ldapConfigFromRequest(req ldapConfigUpdateRequest, current *config.LDAPAuthConfig) *config.LDAPAuthConfig {
	password := req.BindPassword
	if password == "" && current != nil {
		password = current.BindPassword
	}
	return &config.LDAPAuthConfig{
		Enable: req.Enabled, URL: strings.TrimSpace(req.URL), BindDN: strings.TrimSpace(req.BindDN),
		BindPassword: password, BaseDN: strings.TrimSpace(req.BaseDN), UserFilter: strings.TrimSpace(req.UserFilter),
		UsernameAttribute: strings.TrimSpace(req.UsernameAttribute), EmailAttribute: strings.TrimSpace(req.EmailAttribute),
		AutoProvision: req.AutoProvision, StartTLS: req.StartTLS, AllowPlaintext: req.AllowPlaintext,
		TLSServerName: strings.TrimSpace(req.TLSServerName), TLSCAPEM: strings.TrimSpace(req.TLSCAPEM),
	}
}

// TestLDAPConfig probes the submitted form without persisting it. The caller
// must be a SystemAdmin (enforced by the parent route group).
func (h *SystemHandler) TestLDAPConfig(c *gin.Context) {
	var req ldapConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LDAP configuration"})
		return
	}
	current, err := service.ResolveLDAPConfig(c.Request.Context(), h.cfg, h.systemSettingSvc)
	if err != nil && req.BindPassword == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Existing LDAP secret cannot be read; enter a new password"})
		return
	}
	c.JSON(http.StatusOK, ldapauth.ProbeLDAPConnection(ldapConfigFromRequest(req, current)))
}

func (h *SystemHandler) ResetLDAPConfig(c *gin.Context) {
	if h.systemSettingSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LDAP settings service is unavailable"})
		return
	}
	if err := h.systemSettingSvc.Reset(c.Request.Context(), service.LDAPConfigSettingKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot reset LDAP configuration"})
		return
	}
	h.GetLDAPConfig(c)
}
