package middleware

import (
	"context"
	"errors"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/oidcbearer"
	"github.com/dbre-maestro/maestro/internal/repository"
)

// BearerAuthenticator resolves a bearer token that is not a session access
// token (an OIDC token from the IdP) to a local user. nil disables that path.
type BearerAuthenticator interface {
	Authenticate(ctx context.Context, rawToken string) (*model.User, error)
}

// OIDCBearerAuth maps a verified IdP token to an existing user: the bound
// external identity first, then the email. Find-only: onboarding stays with the
// browser SSO login, so a token alone never creates or binds an account.
type OIDCBearerAuth struct {
	Verifier oidcbearer.Verifier
	Users    *repository.UserRepo
	Provider string // external_identity_source written by the SSO login
}

var errBearerNoEmail = errors.New("oidc bearer: token carries no email")

func (a OIDCBearerAuth) Authenticate(ctx context.Context, rawToken string) (*model.User, error) {
	identity, err := a.Verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	user, err := a.Users.GetByExternalIdentity(ctx, a.Provider, identity.Subject)
	if err != nil || user != nil {
		return user, err
	}
	if identity.Email == "" {
		return nil, errBearerNoEmail
	}
	return a.Users.GetByEmail(ctx, identity.Email)
}
