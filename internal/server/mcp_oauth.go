package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"superscience/internal/mcp"
	"superscience/internal/store"
)

const mcpOAuthFlowTTL = 10 * time.Minute

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

type oauthFlowSecret struct {
	CodeVerifier string `json:"codeVerifier"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

func (s *Server) handleMCPOAuthStart(w http.ResponseWriter, r *http.Request, server store.MCPServer) {
	var body struct {
		ClientID     string   `json:"clientId"`
		ClientSecret string   `json:"clientSecret"`
		Scopes       []string `json:"scopes"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ClientID = strings.TrimSpace(body.ClientID)
	if body.ClientID == "" {
		writeError(w, http.StatusBadRequest, "clientId is required for MCP OAuth")
		return
	}
	if server.Transport != "http" && server.Transport != "streamable-http" && server.Transport != "sse" {
		writeError(w, http.StatusBadRequest, "MCP OAuth requires an HTTP or SSE transport")
		return
	}
	resourceMetadata, authorizationMetadata, err := discoverOAuthMetadata(r.Context(), server.URL)
	if err != nil {
		writeStructuredError(w, http.StatusBadGateway, "mcp_oauth_discovery_failed", err.Error(), true, map[string]any{"serverId": server.ID})
		return
	}
	authMethod, err := chooseTokenAuthMethod(authorizationMetadata.TokenEndpointAuthMethodsSupported, body.ClientSecret != "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	scopes, err := selectOAuthScopes(body.Scopes, resourceMetadata.ScopesSupported, authorizationMetadata.ScopesSupported)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := randomOAuthValue(32)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	verifier, err := randomOAuthValue(64)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	redirectURI := callbackURL(r)
	stateHash := oauthStateHash(state)
	flowSecretRef := "mcp-oauth-flow:" + stateHash
	flowSecret, err := json.Marshal(oauthFlowSecret{CodeVerifier: verifier, ClientSecret: body.ClientSecret})
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	if err := s.secrets.Store(flowSecretRef, string(flowSecret)); err != nil {
		writeStructuredError(w, http.StatusServiceUnavailable, "credential_store_unavailable", "could not store the temporary OAuth verifier in the OS credential store", false, map[string]any{"serverId": server.ID})
		return
	}
	scopesJSON, _ := json.Marshal(scopes)
	flow, err := s.store.CreateMCPOAuthFlow(r.Context(), store.MCPOAuthFlow{
		StateHash: stateHash, ServerID: server.ID, ClientID: body.ClientID,
		AuthorizationEndpoint: authorizationMetadata.AuthorizationEndpoint,
		TokenEndpoint:         authorizationMetadata.TokenEndpoint,
		RedirectURI:           redirectURI, Resource: resourceMetadata.Resource, ScopesJSON: string(scopesJSON),
		AuthMethod: authMethod, SecretRef: flowSecretRef, ExpiresAt: time.Now().Add(mcpOAuthFlowTTL).UnixMilli(),
	})
	if err != nil {
		_ = s.secrets.Delete(flowSecretRef)
		writeResult(w, nil, err)
		return
	}
	authorizationURL, err := url.Parse(flow.AuthorizationEndpoint)
	if err != nil {
		_ = s.store.DeleteMCPOAuthFlow(r.Context(), stateHash)
		_ = s.secrets.Delete(flowSecretRef)
		writeResult(w, nil, err)
		return
	}
	query := authorizationURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", flow.ClientID)
	query.Set("redirect_uri", flow.RedirectURI)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("resource", flow.Resource)
	if len(scopes) > 0 {
		query.Set("scope", strings.Join(scopes, " "))
	}
	authorizationURL.RawQuery = query.Encode()
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.oauth.start", EntityID: server.ID, EntityType: "mcp_server"})
	writeJSON(w, http.StatusOK, map[string]any{
		"serverId": server.ID, "state": state, "authorizationUrl": authorizationURL.String(), "status": "pending", "expiresAt": flow.ExpiresAt,
	})
}

func (s *Server) handleMCPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writeError(w, http.StatusBadRequest, "state is required")
		return
	}
	stateHash := oauthStateHash(state)
	flow, err := s.store.GetMCPOAuthFlow(r.Context(), stateHash)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown or expired OAuth state")
		return
	}
	cleanup := func() {
		_ = s.store.DeleteMCPOAuthFlow(context.Background(), stateHash)
		_ = s.secrets.Delete(flow.SecretRef)
	}
	if oauthError := strings.TrimSpace(r.URL.Query().Get("error")); oauthError != "" {
		cleanup()
		writeError(w, http.StatusBadRequest, "OAuth authorization failed: "+oauthError)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		cleanup()
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	encodedFlowSecret, err := s.secrets.Load(flow.SecretRef)
	if err != nil {
		cleanup()
		writeStructuredError(w, http.StatusServiceUnavailable, "credential_store_unavailable", "could not load the OAuth PKCE verifier", false, map[string]any{"serverId": flow.ServerID})
		return
	}
	var flowSecret oauthFlowSecret
	if err := json.Unmarshal([]byte(encodedFlowSecret), &flowSecret); err != nil {
		cleanup()
		writeResult(w, nil, err)
		return
	}
	var scopes []string
	if err := json.Unmarshal([]byte(flow.ScopesJSON), &scopes); err != nil {
		cleanup()
		writeResult(w, nil, err)
		return
	}
	credential, err := mcp.ExchangeOAuthCode(r.Context(), mcp.TokenExchangeRequest{
		TokenEndpoint: flow.TokenEndpoint, ClientID: flow.ClientID, ClientSecret: flowSecret.ClientSecret, AuthMethod: flow.AuthMethod,
		Code: code, CodeVerifier: flowSecret.CodeVerifier, RedirectURI: flow.RedirectURI, Resource: flow.Resource, Scopes: scopes,
	})
	if err != nil {
		cleanup()
		writeStructuredError(w, http.StatusBadGateway, "mcp_oauth_exchange_failed", err.Error(), false, map[string]any{"serverId": flow.ServerID})
		return
	}
	credentialJSON, err := json.Marshal(credential)
	if err != nil {
		cleanup()
		writeResult(w, nil, err)
		return
	}
	credentialRef := "mcp-oauth:" + flow.ServerID
	if err := s.secrets.Store(credentialRef, string(credentialJSON)); err != nil {
		cleanup()
		writeStructuredError(w, http.StatusServiceUnavailable, "credential_store_unavailable", "could not store the MCP OAuth credential", false, map[string]any{"serverId": flow.ServerID})
		return
	}
	server, err := s.store.GetMCPServer(r.Context(), flow.ServerID)
	if err != nil {
		_ = s.secrets.Delete(credentialRef)
		cleanup()
		writeResult(w, nil, err)
		return
	}
	server.OAuthSecretRef = credentialRef
	server.OAuthStatus = "authorized"
	if _, err := s.store.UpsertMCPServer(r.Context(), server); err != nil {
		_ = s.secrets.Delete(credentialRef)
		cleanup()
		writeResult(w, nil, err)
		return
	}
	cleanup()
	_ = mcp.CloseServer(server.ID)
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.oauth.authorized", EntityID: server.ID, EntityType: "mcp_server"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serverId": server.ID, "status": "authorized"})
}

func (s *Server) handleMCPOAuthRemove(w http.ResponseWriter, r *http.Request, server store.MCPServer) {
	if server.OAuthSecretRef != "" {
		if err := s.secrets.Delete(server.OAuthSecretRef); err != nil {
			writeStructuredError(w, http.StatusServiceUnavailable, "credential_store_unavailable", "could not remove the MCP OAuth credential", false, map[string]any{"serverId": server.ID})
			return
		}
	}
	server.OAuthSecretRef = ""
	server.OAuthStatus = ""
	if _, err := s.store.UpsertMCPServer(r.Context(), server); err != nil {
		writeResult(w, nil, err)
		return
	}
	_ = mcp.CloseServer(server.ID)
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.oauth.remove", EntityID: server.ID, EntityType: "mcp_server"})
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "serverId": server.ID})
}

func (s *Server) authenticatedMCPServer(ctx context.Context, server store.MCPServer) (store.MCPServer, error) {
	return mcp.PrepareAuthenticatedServer(ctx, server, s.secrets)
}

func discoverOAuthMetadata(ctx context.Context, resourceURL string) (protectedResourceMetadata, authorizationServerMetadata, error) {
	parsedResource, err := url.Parse(resourceURL)
	if err != nil || parsedResource.Host == "" {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("invalid MCP resource URL")
	}
	if err := validateOAuthURL(parsedResource); err != nil {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, err
	}
	client := oauthDiscoveryClient(parsedResource)
	resourceCandidates := wellKnownCandidates(parsedResource, "oauth-protected-resource")
	var resourceMetadata protectedResourceMetadata
	var discoveryErr error
	for _, candidate := range resourceCandidates {
		discoveryErr = getOAuthJSON(ctx, client, candidate, &resourceMetadata)
		if discoveryErr == nil {
			break
		}
	}
	if discoveryErr != nil {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, fmt.Errorf("protected resource metadata: %w", discoveryErr)
	}
	if resourceMetadata.Resource != resourceURL {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, fmt.Errorf("protected resource metadata identifies %q instead of %q", resourceMetadata.Resource, resourceURL)
	}
	if len(resourceMetadata.AuthorizationServers) == 0 {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("protected resource metadata has no authorization server")
	}
	issuer, err := url.Parse(resourceMetadata.AuthorizationServers[0])
	if err != nil || issuer.Host == "" {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("invalid OAuth issuer")
	}
	if err := validateOAuthURL(issuer); err != nil {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, err
	}
	metadataCandidates := append(wellKnownCandidates(issuer, "oauth-authorization-server"), wellKnownCandidates(issuer, "openid-configuration")...)
	var authorizationMetadata authorizationServerMetadata
	discoveryErr = nil
	for _, candidate := range metadataCandidates {
		discoveryErr = getOAuthJSON(ctx, client, candidate, &authorizationMetadata)
		if discoveryErr == nil {
			break
		}
	}
	if discoveryErr != nil {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, fmt.Errorf("authorization server metadata: %w", discoveryErr)
	}
	if authorizationMetadata.Issuer != issuer.String() {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("authorization metadata issuer mismatch")
	}
	if !slices.Contains(authorizationMetadata.CodeChallengeMethodsSupported, "S256") {
		return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("authorization server does not support PKCE S256")
	}
	for _, endpoint := range []string{authorizationMetadata.AuthorizationEndpoint, authorizationMetadata.TokenEndpoint} {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Host == "" {
			return protectedResourceMetadata{}, authorizationServerMetadata{}, errors.New("authorization server metadata contains an invalid endpoint")
		}
		if err := validateOAuthURL(parsed); err != nil {
			return protectedResourceMetadata{}, authorizationServerMetadata{}, err
		}
	}
	return resourceMetadata, authorizationMetadata, nil
}

func wellKnownCandidates(endpoint *url.URL, name string) []string {
	pathSpecific := *endpoint
	pathSpecific.RawQuery = ""
	pathSpecific.Fragment = ""
	pathSpecific.Path = path.Join("/.well-known/"+name, endpoint.Path)
	root := *endpoint
	root.RawQuery = ""
	root.Fragment = ""
	root.Path = "/.well-known/" + name
	if pathSpecific.String() == root.String() {
		return []string{root.String()}
	}
	return []string{pathSpecific.String(), root.String()}
}

func oauthDiscoveryClient(resource *url.URL) *http.Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 20 * time.Second}).DialContext,
		TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many OAuth metadata redirects")
			}
			return validateOAuthURL(request.URL)
		},
	}
}

func getOAuthJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", endpoint, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1_000_001))
	if err != nil {
		return err
	}
	if len(body) > 1_000_000 {
		return errors.New("OAuth metadata exceeds size limit")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	return nil
}

func validateOAuthURL(endpoint *url.URL) error {
	if endpoint.User != nil || endpoint.Fragment != "" {
		return errors.New("OAuth URLs cannot contain credentials or fragments")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	host := endpoint.Hostname()
	if endpoint.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return nil
	}
	return errors.New("OAuth URLs must use HTTPS unless they are loopback")
}

func chooseTokenAuthMethod(supported []string, hasSecret bool) (string, error) {
	if hasSecret {
		if len(supported) == 0 || slices.Contains(supported, "client_secret_basic") {
			return "client_secret_basic", nil
		}
		if slices.Contains(supported, "client_secret_post") {
			return "client_secret_post", nil
		}
		return "", errors.New("authorization server does not support a compatible client secret method")
	}
	if len(supported) == 0 || slices.Contains(supported, "none") {
		return "none", nil
	}
	return "", errors.New("authorization server requires a client secret")
}

func selectOAuthScopes(requested, resourceSupported, authorizationSupported []string) ([]string, error) {
	if len(requested) == 0 {
		requested = append([]string(nil), resourceSupported...)
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(requested))
	for _, scope := range requested {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		if len(resourceSupported) > 0 && !slices.Contains(resourceSupported, scope) {
			return nil, fmt.Errorf("scope %q is not supported by the MCP resource", scope)
		}
		if len(authorizationSupported) > 0 && !slices.Contains(authorizationSupported, scope) {
			return nil, fmt.Errorf("scope %q is not supported by the authorization server", scope)
		}
		seen[scope] = true
		result = append(result, scope)
	}
	return result, nil
}

func callbackURL(r *http.Request) string {
	return "http://" + r.Host + "/api/mcp/oauth/callback"
}

func randomOAuthValue(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func oauthStateHash(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}
