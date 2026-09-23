package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/Tencent/WeKnora/internal/config"
)

var ErrInvalidLDAPCredentials = errors.New("invalid LDAP credentials")

type LDAPIdentity struct {
	Subject  string
	Username string
	Email    string
}

// Authenticate validates credentials against LDAP/Active Directory and
// returns the normalized local identity fields used by WeKnora.
func Authenticate(username, password string, cfg *config.LDAPAuthConfig) (*LDAPIdentity, error) {
	if cfg == nil || !cfg.Enable || strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("LDAP authentication is not enabled")
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidLDAPCredentials
	}

	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Path != "" {
		return nil, fmt.Errorf("invalid LDAP URL")
	}
	if parsed.Scheme != "ldaps" && !(parsed.Scheme == "ldap" && cfg.StartTLS) {
		return nil, errors.New("LDAP requires LDAPS or StartTLS")
	}
	tlsConfig := &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12}
	conn, err := ldap.DialURL(cfg.URL,
		ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}),
		ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("connect LDAP: %w", err)
	}
	defer conn.Close()
	conn.SetTimeout(10 * time.Second)
	if cfg.StartTLS && parsed.Scheme == "ldap" {
		if err := conn.StartTLS(tlsConfig); err != nil {
			return nil, fmt.Errorf("start LDAP TLS: %w", err)
		}
	}

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("bind LDAP service account: %w", err)
		}
	}
	filter := strings.ReplaceAll(cfg.UserFilter, "{username}", ldap.EscapeFilter(username))
	attrs := uniqueStrings(cfg.UsernameAttribute, cfg.EmailAttribute)
	result, err := conn.Search(ldap.NewSearchRequest(cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, filter, attrs, nil))
	if err != nil {
		return nil, fmt.Errorf("search LDAP user: %w", err)
	}
	if len(result.Entries) != 1 {
		return nil, ErrInvalidLDAPCredentials
	}
	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, ErrInvalidLDAPCredentials
	}
	identity := &LDAPIdentity{
		Subject:  entry.DN,
		Username: strings.TrimSpace(entry.GetAttributeValue(cfg.UsernameAttribute)),
		Email:    strings.TrimSpace(entry.GetAttributeValue(cfg.EmailAttribute)),
	}
	if identity.Username == "" {
		identity.Username = username
	}
	if identity.Subject == "" {
		return nil, errors.New("LDAP entry has no DN")
	}
	address, err := mail.ParseAddress(identity.Email)
	if err != nil || address.Address != identity.Email {
		return nil, errors.New("LDAP entry has no valid email")
	}
	return identity, nil
}

func uniqueStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}
