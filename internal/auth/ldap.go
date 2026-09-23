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

type ConnectionProbe struct {
	OK      bool   `json:"ok"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// ProbeLDAPConnection checks TLS, service-account bind and Base DN lookup.
// It does not persist the configuration or authenticate an end user.
func ProbeLDAPConnection(cfg *config.LDAPAuthConfig) ConnectionProbe {
	if cfg == nil {
		return ConnectionProbe{Stage: "config", Message: "LDAP 配置不能为空"}
	}
	check := *cfg
	check.Enable = true
	if err := config.ValidateConfig(&config.Config{LDAPAuth: &check}); err != nil {
		return ConnectionProbe{Stage: "config", Message: err.Error()}
	}
	conn, stage, err := connectAndBindService(&check)
	if err != nil {
		switch stage {
		case "bind":
			return ConnectionProbe{Stage: stage, Message: "目录搜索账号绑定失败，请核对 Bind DN 和密码"}
		case "tls":
			return ConnectionProbe{Stage: stage, Message: "TLS 验证失败：" + err.Error()}
		default:
			return ConnectionProbe{Stage: stage, Message: "LDAP 连接失败：" + err.Error()}
		}
	}
	defer conn.Close()
	result, err := conn.Search(ldap.NewSearchRequest(
		check.BaseDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 10, false,
		"(objectClass=*)", []string{"1.1"}, nil,
	))
	if err != nil || len(result.Entries) != 1 {
		return ConnectionProbe{Stage: "search", Message: "目录已连接，但用户 Base DN 无法读取，请核对搜索范围和服务账号权限"}
	}
	return ConnectionProbe{OK: true, Stage: "ready", Message: "连接、服务账号绑定和 Base DN 读取成功；尚未验证用户密码"}
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

	conn, _, err := connectAndBindService(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
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

func connectAndBindService(cfg *config.LDAPAuthConfig) (*ldap.Conn, string, error) {
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Path != "" {
		return nil, "config", fmt.Errorf("invalid LDAP URL")
	}
	if parsed.Scheme != "ldaps" && !(parsed.Scheme == "ldap" && cfg.StartTLS) {
		return nil, "config", errors.New("LDAP requires LDAPS or StartTLS")
	}
	tlsConfig := &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12}
	conn, err := ldap.DialURL(cfg.URL,
		ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}),
		ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, "connect", fmt.Errorf("connect LDAP: %w", err)
	}
	conn.SetTimeout(10 * time.Second)
	if cfg.StartTLS && parsed.Scheme == "ldap" {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, "tls", fmt.Errorf("start LDAP TLS: %w", err)
		}
	}

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			conn.Close()
			return nil, "bind", fmt.Errorf("bind LDAP service account: %w", err)
		}
	}
	return conn, "ready", nil
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
