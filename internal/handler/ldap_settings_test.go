package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type ldapSettingServiceStub struct {
	interfaces.SystemSettingService
	value string
}

func (s *ldapSettingServiceStub) GetString(context.Context, string, string, string) string {
	return s.value
}
func (s *ldapSettingServiceStub) Update(_ context.Context, _ string, value any) (*types.SystemSetting, error) {
	s.value = value.(string)
	return &types.SystemSetting{}, nil
}
func (s *ldapSettingServiceStub) Reset(context.Context, string) error {
	s.value = ""
	return nil
}

func TestLDAPSettingsEndpointRedactsSecretAndAppliesRuntimeConfig(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	gin.SetMode(gin.TestMode)
	settings := &ldapSettingServiceStub{}
	h := &SystemHandler{cfg: &config.Config{LDAPAuth: &config.LDAPAuthConfig{}}, systemSettingSvc: settings}
	r := gin.New()
	r.GET("/ldap", h.GetLDAPConfig)
	r.PUT("/ldap", h.UpdateLDAPConfig)
	r.DELETE("/ldap", h.ResetLDAPConfig)

	request := `{"enabled":true,"url":"ldaps://directory.example.com:636","bind_dn":"cn=service,dc=example,dc=com","bind_password":"test-directory-password","base_dn":"dc=example,dc=com","user_filter":"(uid={username})","username_attribute":"uid","email_attribute":"mail","auto_provision":true}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/ldap", strings.NewReader(request)))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "test-directory-password") {
		t.Fatalf("LDAP config save failed or echoed secret: status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(settings.value, "test-directory-password") {
		t.Fatal("stored LDAP settings contained plaintext bind password")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ldap", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":true`) ||
		!strings.Contains(w.Body.String(), `"has_bind_password":true`) || strings.Contains(w.Body.String(), "test-directory-password") {
		t.Fatalf("LDAP config read was not redacted: status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/ldap", nil))
	if w.Code != http.StatusOK || settings.value != "" || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("LDAP config reset failed: status=%d body=%s", w.Code, w.Body.String())
	}
}
