package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type ldapSettingsStub struct {
	interfaces.SystemSettingService
	value string
}

func (s *ldapSettingsStub) GetString(context.Context, string, string, string) string { return s.value }

func (s *ldapSettingsStub) Update(_ context.Context, _ string, value any) (*types.SystemSetting, error) {
	s.value = value.(string)
	return &types.SystemSetting{}, nil
}

func TestLDAPConfigSavesEncryptedSecretAndResolvesRuntimeOverride(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	settings := &ldapSettingsStub{}
	want := &config.LDAPAuthConfig{
		Enable: true, URL: "ldaps://directory.example.com:636", BindDN: "cn=service,dc=example,dc=com",
		BindPassword: "directory-secret", BaseDN: "dc=example,dc=com", UserFilter: "(uid={username})",
		UsernameAttribute: "uid", EmailAttribute: "mail", AutoProvision: true, AllowPlaintext: true,
	}
	if err := SaveLDAPConfig(context.Background(), settings, want); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(settings.value, want.BindPassword) || !strings.Contains(settings.value, "enc:v1:") {
		t.Fatal("stored LDAP configuration exposed the bind password")
	}
	got, err := ResolveLDAPConfig(context.Background(), &config.Config{}, settings)
	if err != nil || got.BindPassword != want.BindPassword || !got.Enable || !got.AutoProvision || !got.AllowPlaintext {
		t.Fatalf("runtime LDAP config was not restored: enabled=%v err=%v", got != nil && got.Enable, err)
	}
}

func TestLDAPConfigRefusesPlaintextStorageWithoutEncryptionKey(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "")
	settings := &ldapSettingsStub{}
	err := SaveLDAPConfig(context.Background(), settings, &config.LDAPAuthConfig{
		Enable: true, URL: "ldaps://directory.example.com:636", BindDN: "cn=service,dc=example,dc=com",
		BindPassword: "directory-secret", BaseDN: "dc=example,dc=com", UserFilter: "(uid={username})",
	})
	if err == nil || settings.value != "" {
		t.Fatalf("LDAP secret must not be persisted without SYSTEM_AES_KEY: %v", err)
	}
}
