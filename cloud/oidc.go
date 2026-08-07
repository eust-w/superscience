package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrOIDCToken    = errors.New("invalid OIDC token")
	ErrOIDCIdentity = errors.New("invalid OIDC identity")
)

type OIDCConfig struct {
	Issuer              string
	ClientID            string
	AllowedEmailDomains []string
	HTTPClient          *http.Client
}

type OIDCIdentity struct {
	Actor       string
	Subject     string
	DisplayName string
	Email       string
}

type OIDCAuthenticator interface {
	Verify(context.Context, string) (OIDCIdentity, error)
	Health(context.Context) error
}

type oidcAuthenticator struct {
	issuer         string
	client         *http.Client
	verifier       *oidc.IDTokenVerifier
	allowedDomains map[string]bool
}

func NewOIDCAuthenticator(ctx context.Context, config OIDCConfig) (OIDCAuthenticator, error) {
	issuer, err := validateIssuer(config.Issuer)
	if err != nil {
		return nil, err
	}
	clientID := strings.TrimSpace(config.ClientID)
	if clientID == "" {
		return nil, errors.New("OIDC client ID is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	discoveryCtx := oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(discoveryCtx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	domains := make(map[string]bool, len(config.AllowedEmailDomains))
	for _, domain := range config.AllowedEmailDomains {
		domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
		if domain == "" || strings.ContainsAny(domain, "@/\\") {
			return nil, fmt.Errorf("invalid allowed OIDC email domain %q", domain)
		}
		domains[domain] = true
	}
	return &oidcAuthenticator{
		issuer: issuer, client: client,
		verifier:       provider.VerifierContext(discoveryCtx, &oidc.Config{ClientID: clientID}),
		allowedDomains: domains,
	}, nil
}

func (a *oidcAuthenticator) Verify(ctx context.Context, rawToken string) (OIDCIdentity, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || len(rawToken) > 64*1024 {
		return OIDCIdentity{}, fmt.Errorf("%w: ID token is empty or too large", ErrOIDCToken)
	}
	token, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("%w: %v", ErrOIDCToken, err)
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := token.Claims(&claims); err != nil {
		return OIDCIdentity{}, fmt.Errorf("%w: decode claims: %v", ErrOIDCToken, err)
	}
	subject := strings.TrimSpace(token.Subject)
	if subject == "" {
		return OIDCIdentity{}, fmt.Errorf("%w: subject is required", ErrOIDCIdentity)
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if len(a.allowedDomains) > 0 {
		separator := strings.LastIndexByte(email, '@')
		if !claims.EmailVerified || separator <= 0 || !a.allowedDomains[strings.Trim(email[separator+1:], ".")] {
			return OIDCIdentity{}, fmt.Errorf("%w: a verified email in an allowed domain is required", ErrOIDCIdentity)
		}
	}
	displayName := strings.TrimSpace(claims.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(claims.PreferredUsername)
	}
	if displayName == "" {
		displayName = email
	}
	if displayName == "" {
		displayName = subject
	}
	digest := sha256.Sum256([]byte(a.issuer + "\x00" + subject))
	return OIDCIdentity{
		Actor: "oidc:" + base64.RawURLEncoding.EncodeToString(digest[:]), Subject: subject,
		DisplayName: displayName, Email: email,
	}, nil
}

func (a *oidcAuthenticator) Health(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	response, err := a.client.Do(request)
	if err != nil {
		return fmt.Errorf("OIDC discovery health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OIDC discovery health returned HTTP %d", response.StatusCode)
	}
	var document struct {
		Issuer string `json:"issuer"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 1024*1024))
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode OIDC discovery health response: %w", err)
	}
	if document.Issuer != a.issuer {
		return fmt.Errorf("OIDC discovery issuer mismatch: expected %q", a.issuer)
	}
	return nil
}

func validateIssuer(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OIDC issuer must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || (host != "localhost" && !isLoopbackIP(host)) {
			return "", errors.New("OIDC issuer must use HTTPS except for loopback development issuers")
		}
	}
	return value, nil
}

func isLoopbackIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
