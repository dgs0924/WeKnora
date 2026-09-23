package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	ldapauth "github.com/Tencent/WeKnora/internal/auth"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

// ldapSubjectLookup is implemented by the production repository. Keeping it
// separate preserves the UserRepository contract used by older test fixtures.
type ldapSubjectLookup interface {
	GetUserByLDAPSubject(context.Context, string) (*types.User, error)
}

var (
	ErrLDAPLinkCredentials = errors.New("local password or LDAP credentials are invalid")
	ErrLDAPLinkConflict    = errors.New("LDAP identity cannot be linked to this account")
)

const LDAPAccountConflictMessage = "LDAP account conflicts with an existing user"

// LinkLDAPAccount requires proof of both the current local password and the
// directory password before attaching an LDAP identity to an existing user.
// Email matching alone never grants access to a local account.
func (s *userService) LinkLDAPAccount(ctx context.Context, userID, localPassword, username, ldapPassword string) error {
	if userID == "" || localPassword == "" || strings.TrimSpace(username) == "" || ldapPassword == "" {
		return ErrLDAPLinkCredentials
	}
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil || user == nil || !user.IsActive {
		return ErrLDAPLinkCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(localPassword)) != nil {
		return ErrLDAPLinkCredentials
	}
	cfg, err := ResolveLDAPConfig(ctx, s.config, s.systemSettingSvc)
	if err != nil {
		return err
	}
	if !cfg.Enable {
		return errors.New("LDAP login is not enabled")
	}
	authenticate := s.ldapAuthenticator
	if authenticate == nil {
		authenticate = ldapauth.Authenticate
	}
	identity, err := authenticate(username, ldapPassword, cfg)
	if errors.Is(err, ldapauth.ErrInvalidLDAPCredentials) {
		return ErrLDAPLinkCredentials
	}
	if err != nil {
		return fmt.Errorf("LDAP authentication unavailable: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(user.Email), strings.TrimSpace(identity.Email)) {
		return ErrLDAPLinkConflict
	}
	lookup, ok := s.userRepo.(ldapSubjectLookup)
	if !ok {
		return errors.New("LDAP subject lookup is not configured")
	}
	owner, err := lookup.GetUserByLDAPSubject(ctx, identity.Subject)
	if err != nil && !isUserLookupNotFound(err) {
		return fmt.Errorf("lookup LDAP account: %w", err)
	}
	if owner != nil {
		if owner.ID == user.ID {
			return nil
		}
		return ErrLDAPLinkConflict
	}
	if user.LDAPSubject != nil && *user.LDAPSubject != identity.Subject {
		return ErrLDAPLinkConflict
	}
	user.LDAPSubject = &identity.Subject
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("save LDAP account link: %w", err)
	}
	return nil
}

func (s *userService) LoginWithLDAP(ctx context.Context, username, password string) (*types.LoginResponse, error) {
	cfg, err := ResolveLDAPConfig(ctx, s.config, s.systemSettingSvc)
	if err != nil {
		return nil, err
	}
	if !cfg.Enable {
		return nil, errors.New("LDAP login is not enabled")
	}
	authenticate := s.ldapAuthenticator
	if authenticate == nil {
		authenticate = ldapauth.Authenticate
	}
	identity, err := authenticate(username, password, cfg)
	if errors.Is(err, ldapauth.ErrInvalidLDAPCredentials) {
		return &types.LoginResponse{Success: false, Message: "Invalid username or password"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LDAP authentication unavailable: %w", err)
	}
	lookup, ok := s.userRepo.(ldapSubjectLookup)
	if !ok {
		return nil, errors.New("LDAP subject lookup is not configured")
	}
	user, err := lookup.GetUserByLDAPSubject(ctx, identity.Subject)
	if err != nil && !isUserLookupNotFound(err) {
		return nil, fmt.Errorf("lookup LDAP account: %w", err)
	}
	if user == nil {
		if !cfg.AutoProvision {
			return &types.LoginResponse{Success: false, Message: "LDAP account is not provisioned"}, nil
		}
		// Email uniqueness deliberately rejects collisions with local/OIDC
		// accounts. Directory operators cannot claim their privileges by
		// asserting the same email address.
		if existing, lookupErr := s.userRepo.GetUserByEmail(ctx, identity.Email); lookupErr == nil && existing != nil {
			return &types.LoginResponse{Success: false, Message: LDAPAccountConflictMessage}, nil
		} else if lookupErr != nil && !isUserLookupNotFound(lookupErr) {
			return nil, fmt.Errorf("lookup LDAP email: %w", lookupErr)
		}
		randomPassword, err := generateRandomString(32)
		if err != nil {
			return nil, fmt.Errorf("generate LDAP account password: %w", err)
		}
		provisioning := types.TenantProvisioningCreatePersonal
		if s.config != nil && s.config.Auth != nil && s.config.Auth.DefaultTenantMode == config.AuthDefaultTenantModeTenantless {
			provisioning = types.TenantProvisioningTenantless
		}
		username := s.generateOIDCUsername(ctx, &types.OIDCUserInfo{Username: identity.Username, Email: identity.Email})
		user, err = s.Register(ctx, &types.RegisterRequest{
			Username: username, Email: identity.Email, Password: randomPassword,
			TenantProvisioning: provisioning, LDAPSubject: &identity.Subject,
		})
		if err != nil {
			return nil, fmt.Errorf("provision LDAP account: %w", err)
		}
	}
	if !user.IsActive {
		return &types.LoginResponse{Success: false, Message: "Account is disabled"}, nil
	}
	return s.loginAuthenticatedUser(ctx, user)
}
