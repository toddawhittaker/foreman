package config

import (
	"fmt"
	"strings"
	"testing"
)

const (
	testUser  = "alice-portal-name"
	testToken = "tokentokentokentoken"
	testKey   = "keykeykeykeykeykeykeykeykeykeykey"
)

func TestSecretsFromEnvironment(t *testing.T) {
	c, err := load(fakeEnv(map[string]string{
		"FOREMAN_FACTORIO_USERNAME": testUser,
		"FOREMAN_FACTORIO_TOKEN":    testToken,
		"FOREMAN_SECRET_KEY":        testKey,
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.FactorioUsername.Reveal() != testUser || c.FactorioToken.Reveal() != testToken || c.SecretKey.Reveal() != testKey {
		t.Error("values were not read")
	}
}

func TestSecretsFromFiles(t *testing.T) {
	c, err := load(fakeEnv(map[string]string{
		"FOREMAN_FACTORIO_USERNAME_FILE": "/run/u",
		"FOREMAN_FACTORIO_TOKEN_FILE":    "/run/t",
		"FOREMAN_SECRET_KEY_FILE":        "/run/k",
	}, map[string]string{"/run/u": testUser + "\n", "/run/t": testToken + "\n", "/run/k": testKey + "\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.FactorioUsername.Reveal() != testUser || c.FactorioToken.Reveal() != testToken || c.SecretKey.Reveal() != testKey {
		t.Error("values were not read from the files")
	}
}

func TestSecretsUnsetByDefault(t *testing.T) {
	c, err := load(fakeEnv(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.FactorioUsername.Reveal() != "" || c.FactorioToken.Reveal() != "" || c.SecretKey.Reveal() != "" {
		t.Error("expected nothing set")
	}
}

func TestSecretsErrors(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"username only", map[string]string{"FOREMAN_FACTORIO_USERNAME": testUser}, "must be set together"},
		{"token only", map[string]string{"FOREMAN_FACTORIO_TOKEN": testToken}, "must be set together"},
		{"short key", map[string]string{"FOREMAN_SECRET_KEY": "short-key-value"}, "FOREMAN_SECRET_KEY must be at least 32 characters"},
		{"both forms of the key", map[string]string{"FOREMAN_SECRET_KEY": testKey, "FOREMAN_SECRET_KEY_FILE": "/run/k"}, "FOREMAN_SECRET_KEY and FOREMAN_SECRET_KEY_FILE are both set"},
		{"missing token file", map[string]string{"FOREMAN_FACTORIO_USERNAME": testUser, "FOREMAN_FACTORIO_TOKEN_FILE": "/run/none"}, "FOREMAN_FACTORIO_TOKEN_FILE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(fakeEnv(tt.vars, nil))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			for _, v := range []string{testUser, testToken, testKey, "short-key-value"} {
				if strings.Contains(err.Error(), v) {
					t.Errorf("error carries a value: %v", err)
				}
			}
		})
	}
}

func TestSecretsNeverFormatted(t *testing.T) {
	c, err := load(fakeEnv(map[string]string{
		"FOREMAN_FACTORIO_USERNAME": testUser,
		"FOREMAN_FACTORIO_TOKEN":    testToken,
		"FOREMAN_SECRET_KEY":        testKey,
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{
		formatAll(c), formatAll(*c), formatAll(c.FactorioToken), formatAll(c.SecretKey),
	} {
		for _, v := range []string{testUser, testToken, testKey} {
			if strings.Contains(out, v) {
				t.Errorf("formatted config carries %q", v)
			}
		}
	}
}

func formatAll(x any) string {
	return fmt.Sprintf("%v|%+v|%#v|%s|%q", x, x, x, x, x)
}
