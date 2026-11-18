package idp

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The fake must refuse what a real provider refuses, or Foreman's tests
// could pass against it while a real one says no.
func TestTokenEndpointChecksClientAndPKCE(t *testing.T) {
	f, err := Start(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const verifier = "a-verifier-that-is-long-enough-for-the-test-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	code := func() string {
		q := url.Values{
			"response_type": {"code"}, "client_id": {ClientID}, "state": {"s"}, "nonce": {"n"},
			"redirect_uri": {"https://foreman.example.com/cb"}, "code_challenge_method": {"S256"},
			"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		}
		resp, err := f.Client.Get(f.URL + "/auth?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		return loc.Query().Get("code")
	}
	exchange := func(code, secret, verifier string) int {
		form := url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"https://foreman.example.com/cb"},
			"client_id": {ClientID}, "client_secret": {secret}, "code_verifier": {verifier},
		}
		resp, err := f.Client.Post(f.URL+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := exchange(code(), "wrong", verifier); got != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", got)
	}
	if got := exchange(code(), ClientSecret, "another-verifier"); got != http.StatusBadRequest {
		t.Errorf("wrong verifier: %d", got)
	}
	c := code()
	if got := exchange(c, ClientSecret, verifier); got != http.StatusOK {
		t.Errorf("good exchange: %d", got)
	}
	if got := exchange(c, ClientSecret, verifier); got != http.StatusBadRequest {
		t.Errorf("reused code: %d", got)
	}
	if f.Tokens() != 1 {
		t.Errorf("tokens issued: %d", f.Tokens())
	}
}
