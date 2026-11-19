package config

import (
	"fmt"
	"net/url"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// OIDC is the identity provider Foreman signs users in through (SPEC 12.7).
type OIDC struct {
	// Issuer is the provider's issuer address, always https, kept exactly
	// as given: the ID token's iss must equal it.
	Issuer        string
	ClientID      string
	ClientSecret  secret.Secret
	RequiredGroup string
}

// loadOIDC reads the identity provider's settings. Each is required when
// oidc is on; without a required group, anyone the provider signs in would
// become an admin (SPEC 12.7). The provider is never called here: Foreman
// fetches its discovery document at the first sign-in (SPEC 3.7).
func loadOIDC(e env, c *Config, errs *[]error) {
	fail := func(format string, args ...any) {
		*errs = append(*errs, fmt.Errorf(format, args...))
	}
	if raw, ok := e.get(varOIDCIssuer); !ok {
		fail("%s must be set when %s includes oidc", varOIDCIssuer, varAuthMethods)
	} else if err := checkIssuer(raw); err != nil {
		fail("%s %v", varOIDCIssuer, err)
	} else {
		c.OIDC.Issuer = raw
	}
	if v, ok := e.get(varOIDCClientID); ok {
		c.OIDC.ClientID = v
	} else {
		fail("%s must be set when %s includes oidc", varOIDCClientID, varAuthMethods)
	}
	switch s, ok, err := e.lookupSecret(varOIDCSecret); {
	case err != nil:
		*errs = append(*errs, err)
	case !ok:
		fail("%s or %s_FILE must be set when %s includes oidc", varOIDCSecret, varOIDCSecret, varAuthMethods)
	default:
		c.OIDC.ClientSecret = s
	}
	if v, ok := e.get(varOIDCGroup); ok {
		c.OIDC.RequiredGroup = v
	} else {
		fail("%s must be set when %s includes oidc; without it anyone the identity provider signs in could use Foreman", varOIDCGroup, varAuthMethods)
	}
	if _, ok := e.get(varPublicURL); !ok {
		fail("%s must be set when %s includes oidc, because the identity provider sends people back to it", varPublicURL, varAuthMethods)
	}
}

// checkIssuer accepts only an https address with a host and no query,
// fragment or user name, so the client secret never crosses the network in
// the clear (SPEC 12.7).
func checkIssuer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must be an https address with no query, for example https://id.example.com/application/o/foreman/, not %q", raw)
	}
	return nil
}
