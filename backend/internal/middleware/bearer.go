package middleware

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/oidcbearer"
	"github.com/dbre-maestro/maestro/internal/repository"
)

// BearerAuthenticator resolves a bearer token that is not a session access
// token (an OIDC token from the IdP) to a local user. nil disables that path.
type BearerAuthenticator interface {
	Authenticate(ctx context.Context, rawToken string) (*model.User, error)
}

// OIDCBearerAuth maps a verified IdP token to the user already bound to its
// subject by a browser SSO login. Find-only, by bound identity only: a token
// never creates or binds an account, and email is not consulted (the browser
// flow refuses to auto-bind protected users on email; this path has no
// interactive step to make such a match safe).
type OIDCBearerAuth struct {
	Verifier oidcbearer.Verifier
	Users    *repository.UserRepo
	Provider string // external_identity_source written by the SSO login
	// RequiresMFA and TrustsMFA reproduce the browser SSO rule at request time:
	// a user under the MFA policy passes only while the IdP is trusted for MFA.
	RequiresMFA func(ctx context.Context, user *model.User) (bool, error)
	TrustsMFA   func(ctx context.Context) (bool, error)
}

var (
	errBearerNoUser    = errors.New("oidc bearer: no user bound to this subject")
	errBearerProtected = errors.New("oidc bearer: protected users cannot use bearer tokens")
	errBearerMFA       = errors.New("oidc bearer: user requires MFA and the IdP is not trusted for it")
)

func (a OIDCBearerAuth) Authenticate(ctx context.Context, rawToken string) (*model.User, error) {
	identity, err := a.Verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	user, err := a.Users.GetByExternalIdentity(ctx, a.Provider, identity.Subject)
	if err != nil {
		return nil, err
	}
	if user == nil {
		slog.Info("oidc bearer: valid token without a bound user", "email_domain", emailDomain(identity.Email))
		return nil, errBearerNoUser
	}
	if user.IsProtected {
		slog.Info("oidc bearer: protected user denied", "user_id", user.ID)
		return nil, errBearerProtected
	}
	required, err := a.RequiresMFA(ctx, user)
	if err != nil {
		return nil, err
	}
	if required {
		trusted, err := a.TrustsMFA(ctx)
		if err != nil {
			return nil, err
		}
		if !trusted {
			slog.Info("oidc bearer: MFA-required user denied, IdP not trusted for MFA", "user_id", user.ID)
			return nil, errBearerMFA
		}
	}
	return user, nil
}

func emailDomain(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return email[i+1:]
	}
	return ""
}
