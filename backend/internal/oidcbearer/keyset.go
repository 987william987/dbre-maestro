package oidcbearer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/sync/singleflight"
)

// cachedKeySet is an oidc.KeySet that verifies RS256 signatures against the
// issuer's JWKS. A token whose key is not cached (key rollover) triggers one
// refetch, but never more than one per cooldown, so forged tokens cannot make
// this server hammer the IdP. A failed fetch counts toward the cooldown too.
type cachedKeySet struct {
	jwksURL  string
	client   *http.Client
	cooldown time.Duration
	now      func() time.Time

	mu        sync.Mutex
	keys      []jose.JSONWebKey
	fetchedAt time.Time
	fetch     singleflight.Group
}

var errNoMatchingKey = errors.New("oidc bearer: no cached signing key verifies the token")

func (k *cachedKeySet) VerifySignature(ctx context.Context, rawJWT string) ([]byte, error) {
	jws, err := jose.ParseSigned(rawJWT, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, fmt.Errorf("oidc bearer: parse signature: %w", err)
	}
	kid := ""
	if len(jws.Signatures) > 0 {
		kid = jws.Signatures[0].Header.KeyID
	}
	if payload, ok := k.verifyCached(jws, kid); ok {
		return payload, nil
	}
	k.mu.Lock()
	cool := k.now().Sub(k.fetchedAt) < k.cooldown
	k.mu.Unlock()
	if cool {
		return nil, errNoMatchingKey
	}
	if err := k.refresh(ctx); err != nil {
		return nil, err
	}
	if payload, ok := k.verifyCached(jws, kid); ok {
		return payload, nil
	}
	return nil, errNoMatchingKey
}

func (k *cachedKeySet) verifyCached(jws *jose.JSONWebSignature, kid string) ([]byte, bool) {
	k.mu.Lock()
	keys := k.keys
	k.mu.Unlock()
	for i := range keys {
		if kid != "" && keys[i].KeyID != kid {
			continue
		}
		if payload, err := jws.Verify(&keys[i]); err == nil {
			return payload, true
		}
	}
	return nil, false
}

// refresh fetches the JWKS once for all concurrent callers. The fetch itself
// runs on a background context bounded by the client timeout, so one caller
// giving up does not abort a fetch the others wait on; each waiter still
// returns when its own ctx is done.
func (k *cachedKeySet) refresh(ctx context.Context) error {
	ch := k.fetch.DoChan("jwks", func() (any, error) {
		defer func() {
			k.mu.Lock()
			k.fetchedAt = k.now()
			k.mu.Unlock()
		}()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, k.jwksURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := k.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("oidc bearer: fetch jwks: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("oidc bearer: fetch jwks: status %d", resp.StatusCode)
		}
		var set jose.JSONWebKeySet
		if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&set); err != nil {
			return nil, fmt.Errorf("oidc bearer: decode jwks: %w", err)
		}
		k.mu.Lock()
		k.keys = set.Keys
		k.mu.Unlock()
		return nil, nil
	})
	select {
	case res := <-ch:
		return res.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}
