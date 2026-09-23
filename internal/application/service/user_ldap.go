package service

import (
	"context"
	"errors"
	"fmt"

	ldapauth "github.com/Tencent/WeKnora/internal/auth"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

// ldapSubjectLookup is implemented by the production repository. Keeping it
// separate preserves the UserRepository contract used by older test fixtures.
type ldapSubjectLookup interface {
	GetUserByLDAPSubject(context.Context, string) (*types.User, error)
}

func (s *userService) LoginWithLDAP(ctx context.Context, username, password string) (*types.LoginResponse, error) {
	if s.config == nil || s.config.LDAPAuth == nil || !s.config.LDAPAuth.Enable {
		return nil, errors.New("LDAP login is not enabled")
	}
	authenticate := s.ldapAuthenticator
	if authenticate == nil {
		authenticate = ldapauth.Authenticate
	}
	identity, err := authenticate(username, password, s.config.LDAPAuth)
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
		if !s.config.LDAPAuth.AutoProvision {
			return &types.LoginResponse{Success: false, Message: "LDAP account is not provisioned"}, nil
		}
		// Email uniqueness deliberately rejects collisions with local/OIDC
		// accounts. Directory operators cannot claim their privileges by
		// asserting the same email address.
		if existing, lookupErr := s.userRepo.GetUserByEmail(ctx, identity.Email); lookupErr == nil && existing != nil {
			return &types.LoginResponse{Success: false, Message: "LDAP account conflicts with an existing user"}, nil
		} else if lookupErr != nil && !isUserLookupNotFound(lookupErr) {
			return nil, fmt.Errorf("lookup LDAP email: %w", lookupErr)
		}
		randomPassword, err := generateRandomString(32)
		if err != nil {
			return nil, fmt.Errorf("generate LDAP account password: %w", err)
		}
		provisioning := types.TenantProvisioningCreatePersonal
		if s.config.Auth != nil && s.config.Auth.DefaultTenantMode == config.AuthDefaultTenantModeTenantless {
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
