package service

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/Tencent/WeKnora/internal/auth"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type ldapLoginRepo struct {
	interfaces.UserRepository
	subjectUser *types.User
	emailUser   *types.User
	created     *types.User
	byID        *types.User
	updated     *types.User
}

func (r *ldapLoginRepo) GetUserByLDAPSubject(context.Context, string) (*types.User, error) {
	return r.subjectUser, nil
}

func (r *ldapLoginRepo) GetUserByEmail(context.Context, string) (*types.User, error) {
	return r.emailUser, nil
}

func (r *ldapLoginRepo) GetUserByUsername(context.Context, string) (*types.User, error) {
	return nil, nil
}

func (r *ldapLoginRepo) CreateUser(_ context.Context, user *types.User) error {
	r.created = user
	return nil
}

func (r *ldapLoginRepo) GetUserByID(context.Context, string) (*types.User, error) {
	return r.byID, nil
}

func (r *ldapLoginRepo) UpdateUser(_ context.Context, user *types.User) error {
	r.updated = user
	return nil
}

type ldapTokenRepo struct{ interfaces.AuthTokenRepository }

func (*ldapTokenRepo) CreateToken(context.Context, *types.AuthToken) error { return nil }

func newLDAPLoginService(repo *ldapLoginRepo, provision bool) *userService {
	return &userService{
		userRepo: repo,
		config:   &config.Config{LDAPAuth: &config.LDAPAuthConfig{Enable: true, AutoProvision: provision}},
		ldapAuthenticator: func(_, _ string, _ *config.LDAPAuthConfig) (*auth.LDAPIdentity, error) {
			return &auth.LDAPIdentity{Subject: "uid=alice,ou=people,dc=example,dc=com", Username: "alice", Email: "alice@example.com"}, nil
		},
	}
}

func TestLDAPLoginDoesNotClaimLocalAccountByEmail(t *testing.T) {
	svc := newLDAPLoginService(&ldapLoginRepo{emailUser: &types.User{
		ID: "local-admin", Email: "alice@example.com", IsActive: true, IsSystemAdmin: true,
	}}, true)
	response, err := svc.LoginWithLDAP(context.Background(), "alice", "valid-password")
	if err != nil || response == nil || response.Success || response.User != nil {
		t.Fatalf("LDAP email collision must not issue local-admin tokens: response=%+v err=%v", response, err)
	}
}

func TestLDAPLoginRejectsUnknownSubjectWithoutProvisioning(t *testing.T) {
	svc := newLDAPLoginService(&ldapLoginRepo{}, false)
	response, err := svc.LoginWithLDAP(context.Background(), "alice", "valid-password")
	if err != nil || response == nil || response.Success {
		t.Fatalf("unknown LDAP subject must be denied: response=%+v err=%v", response, err)
	}
}

func TestLDAPLoginRejectsBadDirectoryPassword(t *testing.T) {
	svc := newLDAPLoginService(&ldapLoginRepo{}, true)
	svc.ldapAuthenticator = func(_, _ string, _ *config.LDAPAuthConfig) (*auth.LDAPIdentity, error) {
		return nil, auth.ErrInvalidLDAPCredentials
	}
	response, err := svc.LoginWithLDAP(context.Background(), "alice", "bad-password")
	if err != nil || response == nil || response.Success {
		t.Fatalf("bad LDAP password must be denied: response=%+v err=%v", response, err)
	}
}

func TestLDAPLoginSurfacesDirectoryOutage(t *testing.T) {
	svc := newLDAPLoginService(&ldapLoginRepo{}, true)
	svc.ldapAuthenticator = func(_, _ string, _ *config.LDAPAuthConfig) (*auth.LDAPIdentity, error) {
		return nil, errors.New("directory unavailable")
	}
	response, err := svc.LoginWithLDAP(context.Background(), "alice", "valid-password")
	if response != nil || err == nil {
		t.Fatalf("directory outage must not become an invalid-credential response: response=%+v err=%v", response, err)
	}
}

func TestLDAPFirstLoginCreatesBoundAccountAndIssuesTokens(t *testing.T) {
	repo := &ldapLoginRepo{}
	svc := newLDAPLoginService(repo, true)
	svc.config.Auth = &config.AuthConfig{DefaultTenantMode: config.AuthDefaultTenantModeTenantless}
	svc.tokenRepo = &ldapTokenRepo{}
	response, err := svc.LoginWithLDAP(context.Background(), "alice", "valid-password")
	if err != nil || response == nil || !response.Success || response.Token == "" || response.RefreshToken == "" {
		t.Fatalf("LDAP first login should create a usable session: response=%+v err=%v", response, err)
	}
	if repo.created == nil || repo.created.LDAPSubject == nil || *repo.created.LDAPSubject != "uid=alice,ou=people,dc=example,dc=com" {
		t.Fatalf("LDAP identity was not bound to the created account: %+v", repo.created)
	}
	if repo.created.TenantID != 0 || repo.created.PasswordHash == "valid-password" {
		t.Fatalf("LDAP account tenant or password state is wrong: %+v", repo.created)
	}
}

func TestLDAPLinkRequiresBothPasswordsAndMatchingIdentity(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("local-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &ldapLoginRepo{byID: &types.User{ID: "local-user", Email: "alice@example.com", PasswordHash: string(hash), IsActive: true}}
	svc := newLDAPLoginService(repo, true)
	if err := svc.LinkLDAPAccount(context.Background(), "local-user", "wrong", "alice", "directory-pass"); !errors.Is(err, ErrLDAPLinkCredentials) {
		t.Fatalf("wrong local password must not link LDAP account: %v", err)
	}
	if repo.updated != nil {
		t.Fatal("account was modified despite wrong local password")
	}
	svc.ldapAuthenticator = func(_, _ string, _ *config.LDAPAuthConfig) (*auth.LDAPIdentity, error) {
		return nil, auth.ErrInvalidLDAPCredentials
	}
	if err := svc.LinkLDAPAccount(context.Background(), "local-user", "local-pass", "alice", "wrong"); !errors.Is(err, ErrLDAPLinkCredentials) {
		t.Fatalf("wrong LDAP password must not link account: %v", err)
	}
	svc.ldapAuthenticator = func(_, _ string, _ *config.LDAPAuthConfig) (*auth.LDAPIdentity, error) {
		return &auth.LDAPIdentity{Subject: "uid=alice,dc=example,dc=com", Email: "other@example.com"}, nil
	}
	if err := svc.LinkLDAPAccount(context.Background(), "local-user", "local-pass", "alice", "directory-pass"); !errors.Is(err, ErrLDAPLinkConflict) {
		t.Fatalf("mismatched LDAP email must not link account: %v", err)
	}
	if repo.updated != nil {
		t.Fatal("account was modified despite mismatched directory identity")
	}
}

func TestLDAPLinkBindsVerifiedSubject(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("local-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &ldapLoginRepo{byID: &types.User{ID: "local-user", Email: "alice@example.com", PasswordHash: string(hash), IsActive: true}}
	svc := newLDAPLoginService(repo, true)
	if err := svc.LinkLDAPAccount(context.Background(), "local-user", "local-pass", "alice", "directory-pass"); err != nil {
		t.Fatalf("verified LDAP account should link: %v", err)
	}
	if repo.updated == nil || repo.updated.LDAPSubject == nil || *repo.updated.LDAPSubject != "uid=alice,ou=people,dc=example,dc=com" {
		t.Fatalf("LDAP subject was not persisted: %+v", repo.updated)
	}
}
