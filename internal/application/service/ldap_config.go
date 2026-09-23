package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

const LDAPConfigSettingKey = "auth.ldap_config"

type storedLDAPConfig struct {
	Enable            bool   `json:"enable"`
	URL               string `json:"url"`
	BindDN            string `json:"bind_dn"`
	EncryptedPassword string `json:"encrypted_bind_password"`
	BaseDN            string `json:"base_dn"`
	UserFilter        string `json:"user_filter"`
	UsernameAttribute string `json:"username_attribute"`
	EmailAttribute    string `json:"email_attribute"`
	AutoProvision     bool   `json:"auto_provision"`
	StartTLS          bool   `json:"start_tls"`
}

// ResolveLDAPConfig reads the runtime override, falling back to the boot
// configuration. A missing encryption key fails closed when a stored secret
// exists; ciphertext is never treated as a bind password.
func ResolveLDAPConfig(ctx context.Context, base *config.Config, settings interfaces.SystemSettingService) (*config.LDAPAuthConfig, error) {
	fallback := &config.LDAPAuthConfig{}
	if base != nil && base.LDAPAuth != nil {
		copy := *base.LDAPAuth
		fallback = &copy
	}
	if settings == nil {
		return fallback, nil
	}
	raw := settings.GetString(ctx, LDAPConfigSettingKey, "", "")
	if raw == "" {
		return fallback, nil
	}
	var stored storedLDAPConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("decode LDAP configuration: %w", err)
	}
	password := ""
	if stored.EncryptedPassword != "" {
		if !strings.HasPrefix(stored.EncryptedPassword, utils.EncPrefix) || utils.GetAESKey() == nil {
			return nil, errors.New("LDAP bind password cannot be decrypted")
		}
		var err error
		password, err = utils.DecryptAESGCM(stored.EncryptedPassword, utils.GetAESKey())
		if err != nil {
			return nil, fmt.Errorf("decrypt LDAP bind password: %w", err)
		}
	}
	return &config.LDAPAuthConfig{
		Enable: stored.Enable, URL: stored.URL, BindDN: stored.BindDN,
		BindPassword: password, BaseDN: stored.BaseDN, UserFilter: stored.UserFilter,
		UsernameAttribute: stored.UsernameAttribute, EmailAttribute: stored.EmailAttribute,
		AutoProvision: stored.AutoProvision, StartTLS: stored.StartTLS,
	}, nil
}

func SaveLDAPConfig(ctx context.Context, settings interfaces.SystemSettingService, cfg *config.LDAPAuthConfig) error {
	if settings == nil || cfg == nil {
		return errors.New("LDAP settings service is unavailable")
	}
	if err := config.ValidateConfig(&config.Config{LDAPAuth: cfg}); err != nil {
		return err
	}
	stored := storedLDAPConfig{
		Enable: cfg.Enable, URL: cfg.URL, BindDN: cfg.BindDN,
		BaseDN: cfg.BaseDN, UserFilter: cfg.UserFilter,
		UsernameAttribute: cfg.UsernameAttribute, EmailAttribute: cfg.EmailAttribute,
		AutoProvision: cfg.AutoProvision, StartTLS: cfg.StartTLS,
	}
	if cfg.BindPassword != "" {
		key := utils.GetAESKey()
		if key == nil {
			return errors.New("SYSTEM_AES_KEY must be configured to save LDAP bind credentials")
		}
		var err error
		stored.EncryptedPassword, err = utils.EncryptAESGCM(cfg.BindPassword, key)
		if err != nil {
			return fmt.Errorf("encrypt LDAP bind password: %w", err)
		}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = settings.Update(ctx, LDAPConfigSettingKey, string(encoded))
	return err
}
