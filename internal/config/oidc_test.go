package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testOIDCSecret = "client-secret-value-1234"

func oidcVars() map[string]string {
	return map[string]string{
		"FOREMAN_AUTH_METHODS":        "local,oidc",
		"FOREMAN_PUBLIC_URL":          "https://foreman.example.com",
		"FOREMAN_OIDC_ISSUER":         "https://id.example.com/application/o/foreman/",
		"FOREMAN_OIDC_CLIENT_ID":      "foreman",
		"FOREMAN_OIDC_CLIENT_SECRET":  testOIDCSecret,
		"FOREMAN_OIDC_REQUIRED_GROUP": "factorio",
	}
}

func TestLoadOIDC(t *testing.T) {
	c, err := load(oidcVars())
	if err != nil {
		t.Fatal(err)
	}
	if !c.AuthOIDC || !c.AuthLocal || c.AuthHeaders {
		t.Errorf("methods: local=%v headers=%v oidc=%v", c.AuthLocal, c.AuthHeaders, c.AuthOIDC)
	}
	o := c.OIDC
	// The issuer is kept exactly, trailing slash included: iss must equal it.
	if o.Issuer != "https://id.example.com/application/o/foreman/" || o.ClientID != "foreman" ||
		o.ClientSecret.Reveal() != testOIDCSecret || o.RequiredGroup != "factorio" {
		t.Errorf("OIDC = %+v", o)
	}
}

func TestLoadOIDCSecretFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(testOIDCSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vars := oidcVars()
	delete(vars, "FOREMAN_OIDC_CLIENT_SECRET")
	vars["FOREMAN_OIDC_CLIENT_SECRET_FILE"] = path
	c, err := load(vars)
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDC.ClientSecret.Reveal() != testOIDCSecret {
		t.Error("the secret was not read from the file")
	}
}

func TestLoadOIDCOnlyWhenOn(t *testing.T) {
	vars := oidcVars()
	vars["FOREMAN_AUTH_METHODS"] = "local"
	c, err := load(vars)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthOIDC || c.OIDC.Issuer != "" || c.OIDC.ClientSecret.Reveal() != "" {
		t.Errorf("oidc settings loaded while oidc is off: %+v", c.OIDC)
	}
}

// Every missing value is reported at once, each naming its variable and
// none quoting the secret (SPEC 3.2, 12.7).
func TestLoadOIDCErrors(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(map[string]string)
		wants []string
	}{
		{"all missing", func(m map[string]string) {
			for _, k := range []string{"FOREMAN_PUBLIC_URL", "FOREMAN_OIDC_ISSUER", "FOREMAN_OIDC_CLIENT_ID", "FOREMAN_OIDC_CLIENT_SECRET", "FOREMAN_OIDC_REQUIRED_GROUP"} {
				delete(m, k)
			}
		}, []string{
			"FOREMAN_OIDC_ISSUER must be set", "FOREMAN_OIDC_CLIENT_ID must be set",
			"FOREMAN_OIDC_CLIENT_SECRET or FOREMAN_OIDC_CLIENT_SECRET_FILE must be set",
			"FOREMAN_OIDC_REQUIRED_GROUP must be set", "FOREMAN_PUBLIC_URL must be set",
		}},
		{"http issuer", func(m map[string]string) { m["FOREMAN_OIDC_ISSUER"] = "http://id.example.com" }, []string{"FOREMAN_OIDC_ISSUER must be an https address"}},
		{"issuer without host", func(m map[string]string) { m["FOREMAN_OIDC_ISSUER"] = "https://" }, []string{"FOREMAN_OIDC_ISSUER must be an https address"}},
		{"issuer with query", func(m map[string]string) { m["FOREMAN_OIDC_ISSUER"] = "https://id.example.com/?a=b" }, []string{"FOREMAN_OIDC_ISSUER must be an https address"}},
		{"issuer with user", func(m map[string]string) { m["FOREMAN_OIDC_ISSUER"] = "https://u:p@id.example.com" }, []string{"FOREMAN_OIDC_ISSUER must be an https address"}},
		{"both secret forms", func(m map[string]string) { m["FOREMAN_OIDC_CLIENT_SECRET_FILE"] = "/run/secrets/x" }, []string{"FOREMAN_OIDC_CLIENT_SECRET and FOREMAN_OIDC_CLIENT_SECRET_FILE are both set"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := oidcVars()
			tt.edit(vars)
			_, err := load(vars)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tt.wants {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			if strings.Contains(err.Error(), testOIDCSecret) {
				t.Error("the error quotes the client secret")
			}
		})
	}
}
