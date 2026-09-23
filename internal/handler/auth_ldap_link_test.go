package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type ldapLinkHandlerUserService struct {
	interfaces.UserService
	linked bool
}

func (*ldapLinkHandlerUserService) LoginWithLDAP(context.Context, string, string) (*types.LoginResponse, error) {
	return &types.LoginResponse{Success: false, Message: service.LDAPAccountConflictMessage}, nil
}

func (*ldapLinkHandlerUserService) GetCurrentUser(context.Context) (*types.User, error) {
	return &types.User{ID: "local-user", IsActive: true}, nil
}

func (s *ldapLinkHandlerUserService) LinkLDAPAccount(_ context.Context, userID, localPassword, username, ldapPassword string) error {
	if userID != "local-user" || localPassword != "local-test-pass" || username != "alice" || ldapPassword != "directory-test-pass" {
		return service.ErrLDAPLinkCredentials
	}
	s.linked = true
	return nil
}

func TestLDAPLoginConflictAndAuthenticatedLinkResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &ldapLinkHandlerUserService{}
	h := &AuthHandler{configInfo: &config.Config{LDAPAuth: &config.LDAPAuthConfig{Enable: true}}, userService: svc}
	r := gin.New()
	r.POST("/ldap/login", h.LDAPLogin)
	r.POST("/ldap/link", h.LinkLDAPAccount)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ldap/login", strings.NewReader(`{"username":"alice","password":"directory-test-pass"}`)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), service.LDAPAccountConflictMessage) {
		t.Fatalf("LDAP email collision should be a conflict, got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ldap/link", strings.NewReader(`{"local_password":"local-test-pass","username":"alice","password":"directory-test-pass"}`)))
	if w.Code != http.StatusOK || !svc.linked || !strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatalf("verified LDAP link should succeed, got %d %s", w.Code, w.Body.String())
	}
}
