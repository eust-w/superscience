package cloud_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"superscience/cloud"
	"superscience/internal/store"
)

type oidcProtocolIssuer struct {
	server   *httptest.Server
	private  *rsa.PrivateKey
	clientID string
}

func newOIDCProtocolIssuer(t *testing.T, clientID string) *oidcProtocolIssuer {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &oidcProtocolIssuer{private: private, clientID: clientID}
	issuer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeProtocolJSON(t, w, map[string]any{
				"issuer": issuer.server.URL, "authorization_endpoint": issuer.server.URL + "/authorize",
				"token_endpoint": issuer.server.URL + "/token", "jwks_uri": issuer.server.URL + "/jwks",
				"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			writeProtocolJSON(t, w, map[string]any{"keys": []map[string]any{{
				"kty": "RSA", "kid": "superscience-test-key", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(private.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(private.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(issuer.server.Close)
	return issuer
}

func writeProtocolJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func (i *oidcProtocolIssuer) token(t *testing.T, overrides map[string]any) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": i.server.URL, "sub": "researcher-42", "aud": i.clientID,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"email": "scientist@lab.example", "email_verified": true, "name": "Test Scientist",
	}
	for key, value := range overrides {
		claims[key] = value
	}
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "superscience-test-key", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.private, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestCloudOIDCDiscoveryExchangeAndAudienceValidation(t *testing.T) {
	ctx := context.Background()
	issuer := newOIDCProtocolIssuer(t, "superscience-cloud-test")
	authenticator, err := cloud.NewOIDCAuthenticator(ctx, cloud.OIDCConfig{
		Issuer: issuer.server.URL, ClientID: issuer.clientID, AllowedEmailDomains: []string{"lab.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(cloud.New(cloud.Options{Store: db, OIDC: authenticator}).Handler())
	defer ts.Close()

	health := getJSON[map[string]any](t, ts.URL+"/health", "")
	authStatus := health["auth"].(map[string]any)
	if health["status"] != "ready" || authStatus["oidc"] != "ready" || authStatus["local"] != "unavailable" {
		t.Fatalf("health = %+v", health)
	}
	auth := postJSON[map[string]any](t, ts.URL+"/api/auth/oidc", map[string]string{
		"idToken": issuer.token(t, nil), "deviceName": "OIDC workstation",
	}, "")
	if len(auth["token"].(string)) < 40 || auth["user"].(map[string]any)["displayName"] != "Test Scientist" {
		t.Fatalf("OIDC auth = %+v", auth)
	}
	actor := auth["user"].(map[string]any)["actor"].(string)
	if !strings.HasPrefix(actor, "oidc:") || strings.Contains(actor, "researcher-42") {
		t.Fatalf("OIDC actor should be stable and opaque, got %q", actor)
	}
	orgs := getJSON[[]map[string]any](t, ts.URL+"/api/orgs", auth["token"].(string))
	if len(orgs) != 1 || orgs[0]["role"] != "owner" {
		t.Fatalf("OIDC organizations = %+v", orgs)
	}

	status, body := requestJSON(t, http.MethodPost, ts.URL+"/api/auth/oidc", map[string]string{
		"idToken": issuer.token(t, map[string]any{"aud": "another-client"}),
	}, "")
	if status != http.StatusUnauthorized || body["code"] != "invalid_oidc_token" {
		t.Fatalf("wrong audience status=%d body=%+v", status, body)
	}
	status, body = requestJSON(t, http.MethodPost, ts.URL+"/api/auth/oidc", map[string]string{
		"idToken": issuer.token(t, map[string]any{"email_verified": false}),
	}, "")
	if status != http.StatusUnauthorized || body["code"] != "invalid_oidc_identity" {
		t.Fatalf("unverified email status=%d body=%+v", status, body)
	}
}

func TestCloudOIDCRejectsInsecureNonLoopbackIssuer(t *testing.T) {
	_, err := cloud.NewOIDCAuthenticator(context.Background(), cloud.OIDCConfig{Issuer: "http://identity.example.com", ClientID: "client"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure issuer error = %v", err)
	}
}

func TestCloudOIDCEndpointIsUnavailableWhenNotConfigured(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(cloud.New(cloud.Options{Store: db, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	defer ts.Close()
	status, body := requestJSON(t, http.MethodPost, ts.URL+"/api/auth/oidc", map[string]string{"idToken": "not-a-token"}, "")
	if status != http.StatusServiceUnavailable || body["code"] != "oidc_unavailable" {
		t.Fatalf("status=%d body=%+v", status, body)
	}
}

func ExampleOIDCConfig() {
	fmt.Println(cloud.OIDCConfig{Issuer: "https://identity.example.com", ClientID: "superscience"}.Issuer)
	// Output: https://identity.example.com
}
