package config

import (
	"strings"
	"testing"
)

func TestLDAPConfigRejectsCleartextCredentialTransport(t *testing.T) {
	ldap := &LDAPAuthConfig{
		Enable: true, URL: "ldap://directory.example.com:389",
		BindDN: "cn=weknora,dc=example,dc=com", BindPassword: "test-secret",
		BaseDN: "ou=people,dc=example,dc=com", UserFilter: "(uid={username})",
	}
	if err := ValidateConfig(&Config{LDAPAuth: ldap}); err == nil || !strings.Contains(err.Error(), "ldaps://") {
		t.Fatalf("cleartext LDAP must fail validation, got %v", err)
	}
	ldap.StartTLS = true
	if err := ValidateConfig(&Config{LDAPAuth: ldap}); err != nil {
		t.Fatalf("StartTLS LDAP should be accepted, got %v", err)
	}
}

func TestLDAPConfigRequiresSearchCredentialAndEscapableFilter(t *testing.T) {
	ldap := &LDAPAuthConfig{Enable: true, URL: "ldaps://directory.example.com:636", BaseDN: "dc=example,dc=com", UserFilter: "(uid=alice)"}
	err := ValidateConfig(&Config{LDAPAuth: ldap})
	if err == nil || !strings.Contains(err.Error(), "bind_dn") || !strings.Contains(err.Error(), "{username}") {
		t.Fatalf("missing bind credentials and unsafe filter must fail validation, got %v", err)
	}
}
