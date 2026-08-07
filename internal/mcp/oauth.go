package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"superscience/internal/store"
)

type SecretBackend interface {
	Store(account, secret string) error
	Delete(account string) error
	Load(account string) (string, error)
}

type OAuthCredential struct {
	AccessToken   string   `json:"accessToken"`
	TokenType     string   `json:"tokenType"`
	RefreshToken  string   `json:"refreshToken,omitempty"`
	ExpiresAt     int64    `json:"expiresAt,omitempty"`
	TokenEndpoint string   `json:"tokenEndpoint"`
	ClientID      string   `json:"clientId"`
	ClientSecret  string   `json:"clientSecret,omitempty"`
	AuthMethod    string   `json:"authMethod"`
	Resource      string   `json:"resource,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

type TokenExchangeRequest struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	AuthMethod    string
	Code          string
	CodeVerifier  string
	RedirectURI   string
	Resource      string
	Scopes        []string
}

func PrepareAuthenticatedServer(ctx context.Context, server store.MCPServer, backend SecretBackend) (store.MCPServer, error) {
	if strings.TrimSpace(server.OAuthSecretRef) == "" {
		return server, nil
	}
	if backend == nil {
		return store.MCPServer{}, errors.New("MCP OAuth credential backend is unavailable")
	}
	encoded, err := backend.Load(server.OAuthSecretRef)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("load MCP OAuth credential: %w", err)
	}
	var credential OAuthCredential
	if err := json.Unmarshal([]byte(encoded), &credential); err != nil {
		return store.MCPServer{}, fmt.Errorf("decode MCP OAuth credential: %w", err)
	}
	if credential.AccessToken == "" {
		return store.MCPServer{}, errors.New("MCP OAuth credential has no access token")
	}
	if credential.ExpiresAt > 0 && credential.ExpiresAt <= time.Now().Add(time.Minute).UnixMilli() {
		credential, err = refreshOAuthCredential(ctx, credential)
		if err != nil {
			return store.MCPServer{}, err
		}
		updated, marshalErr := json.Marshal(credential)
		if marshalErr != nil {
			return store.MCPServer{}, marshalErr
		}
		if err := backend.Store(server.OAuthSecretRef, string(updated)); err != nil {
			return store.MCPServer{}, fmt.Errorf("store refreshed MCP OAuth credential: %w", err)
		}
	}
	server.BearerToken = credential.AccessToken
	return server, nil
}

func ExchangeOAuthCode(ctx context.Context, request TokenExchangeRequest) (OAuthCredential, error) {
	if strings.TrimSpace(request.Code) == "" || strings.TrimSpace(request.CodeVerifier) == "" {
		return OAuthCredential{}, errors.New("authorization code and PKCE verifier are required")
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {request.Code},
		"code_verifier": {request.CodeVerifier},
		"redirect_uri":  {request.RedirectURI},
	}
	credential := OAuthCredential{
		TokenEndpoint: request.TokenEndpoint,
		ClientID:      request.ClientID, ClientSecret: request.ClientSecret, AuthMethod: request.AuthMethod,
		Resource: request.Resource, Scopes: append([]string(nil), request.Scopes...),
	}
	return exchangeToken(ctx, credential, values)
}

func refreshOAuthCredential(ctx context.Context, credential OAuthCredential) (OAuthCredential, error) {
	if credential.RefreshToken == "" {
		return OAuthCredential{}, errors.New("MCP OAuth access token expired and no refresh token is available")
	}
	values := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {credential.RefreshToken}}
	refreshed, err := exchangeToken(ctx, credential, values)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("refresh MCP OAuth token: %w", err)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = credential.RefreshToken
	}
	return refreshed, nil
}

func exchangeToken(ctx context.Context, credential OAuthCredential, values url.Values) (OAuthCredential, error) {
	if err := validateOAuthEndpoint(credential.TokenEndpoint); err != nil {
		return OAuthCredential{}, err
	}
	if credential.ClientID == "" {
		return OAuthCredential{}, errors.New("OAuth client id is required")
	}
	if credential.Resource != "" {
		values.Set("resource", credential.Resource)
	}
	if len(credential.Scopes) > 0 {
		values.Set("scope", strings.Join(credential.Scopes, " "))
	}
	switch credential.AuthMethod {
	case "client_secret_basic":
		if credential.ClientSecret == "" {
			return OAuthCredential{}, errors.New("OAuth client secret is required for client_secret_basic")
		}
	case "client_secret_post":
		if credential.ClientSecret == "" {
			return OAuthCredential{}, errors.New("OAuth client secret is required for client_secret_post")
		}
		values.Set("client_id", credential.ClientID)
		values.Set("client_secret", credential.ClientSecret)
	case "none", "":
		values.Set("client_id", credential.ClientID)
	default:
		return OAuthCredential{}, fmt.Errorf("unsupported OAuth token authentication method %q", credential.AuthMethod)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return OAuthCredential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if credential.AuthMethod == "client_secret_basic" {
		req.SetBasicAuth(credential.ClientID, credential.ClientSecret)
	}
	response, err := mcpHTTPClient("", nil).Do(req)
	if err != nil {
		return OAuthCredential{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1_000_001))
	if err != nil {
		return OAuthCredential{}, err
	}
	if len(body) > 1_000_000 {
		return OAuthCredential{}, errors.New("OAuth token response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var oauthError struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthError)
		message := strings.TrimSpace(oauthError.ErrorDescription)
		if message == "" {
			message = strings.TrimSpace(oauthError.Error)
		}
		if message == "" {
			message = response.Status
		}
		return OAuthCredential{}, fmt.Errorf("OAuth token endpoint rejected the request: %s", message)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return OAuthCredential{}, err
	}
	if token.AccessToken == "" {
		return OAuthCredential{}, errors.New("OAuth token response has no access_token")
	}
	if token.TokenType != "" && !strings.EqualFold(token.TokenType, "Bearer") {
		return OAuthCredential{}, fmt.Errorf("unsupported OAuth token type %q", token.TokenType)
	}
	credential.AccessToken = token.AccessToken
	credential.TokenType = "Bearer"
	credential.RefreshToken = token.RefreshToken
	if token.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UnixMilli()
	} else {
		credential.ExpiresAt = 0
	}
	if token.Scope != "" {
		credential.Scopes = strings.Fields(token.Scope)
	}
	return credential, nil
}

func validateOAuthEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("invalid OAuth endpoint")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return nil
	}
	return errors.New("OAuth endpoint must use HTTPS unless it is loopback")
}
