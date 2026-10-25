package web

import (
	"RCJVGameMaster/src/config"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Synthetic credentials are deliberately padded to ensure configuration cleanup
// never alters an OAuth client secret, which is an opaque credential.
const authTestSecret = "  synthetic-client-secret\t "

func authTestConfig(provider string) config.AuthConfig {
	return config.AuthConfig{FusionAuthURL: provider, FusionAuthClientId: "application-id", FusionAuthTenantId: "tenant-id", FusionAuthSecret: authTestSecret, OAuthRedirectURI: "http://localhost:2006/auth/callback", FrontendURL: "http://localhost:2006/#/"}
}
func authTestClaims() map[string]any {
	return map[string]any{"active": true, "applicationId": "application-id", "tid": "tenant-id", "sub": "registered-user-id", "email": "user@example.invalid", "preferred_username": "Test User", "roles": []string{"admin"}, "exp": time.Now().Add(5 * time.Minute).Unix()}
}

// Run a real HTTP handler through httptest without opening a listener. This
// keeps protocol tests usable in restricted sandboxes and CI without networking.
type authTestTransport func(*http.Request) (*http.Response, error)

func (f authTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type authTestProvider struct {
	URL    string
	client *http.Client
}

func newAuthTestProvider(handler http.Handler) *authTestProvider {
	return &authTestProvider{URL: "http://fusionauth.example.invalid", client: &http.Client{Transport: authTestTransport(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		response := recorder.Result()
		response.Request = r
		return response, nil
	})}}
}
func (p *authTestProvider) Close() { p.client.CloseIdleConnections() }
func newAuthWithTestProvider(cfg config.AuthConfig, provider *authTestProvider) (*Auth, error) {
	auth, err := NewAuth(cfg)
	if err == nil {
		auth.client = provider.client
	}
	return auth, err
}
func TestAuthTrimsConfigurationWhitespaceAndPreservesSecret(t *testing.T) {
	provider := newAuthTestProvider(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/introspect" {
			t.Errorf("unexpected provider path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != "application-id" {
			t.Error("provider client ID was not normalized")
		}
		if r.Form.Get("client_secret") != authTestSecret {
			t.Error("opaque client secret was modified")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authTestClaims())
	}))
	defer provider.Close()
	cfg := authTestConfig(" \t" + provider.URL + " \n")
	cfg.FusionAuthClientId = " application-id\t"
	cfg.FusionAuthTenantId = "tenant-id "
	cfg.OAuthRedirectURI = "\t" + cfg.OAuthRedirectURI + " "
	cfg.FrontendURL = " " + cfg.FrontendURL + "\n"
	cfg.RequiredRole = " Admin\t"
	auth, err := newAuthWithTestProvider(cfg, provider)
	if err != nil {
		t.Fatalf("valid configuration with surrounding whitespace was rejected: %v", err)
	}
	if auth.cfg.FusionAuthSecret != authTestSecret {
		t.Fatal("configuration normalization modified the client secret")
	}
	app := fiber.New()
	app.Get("/auth/login", auth.Login)
	response, err := app.Test(httptest.NewRequest("GET", "http://localhost:2006/auth/login?returnTo=%2Freferee", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("login redirect returned %d", response.StatusCode)
	}
	target, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("tenantId") != "tenant-id" || target.Query().Get("client_id") != "application-id" || target.Query().Get("redirect_uri") != "http://localhost:2006/auth/callback" {
		t.Fatal("authorize redirect retained whitespace in its configuration parameters")
	}
	if target.Query().Get("code_challenge_method") != "S256" || target.Query().Get("code_challenge") == "" || target.Query().Get("state") == "" {
		t.Fatal("authorize redirect omitted PKCE or OAuth state")
	}
	user, _, err := auth.inspect(context.Background(), "synthetic-access-token")
	if err != nil {
		t.Fatalf("registered admin rejected after configuration normalization: %v", err)
	}
	if user.ID != "registered-user-id" {
		t.Fatal("introspection returned the wrong user")
	}
}
func TestAuthProviderRequestsIncludeTenantAndFormCredentials(t *testing.T) {
	seen := map[string]int{}
	provider := newAuthTestProvider(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path]++
		if r.Method != http.MethodPost {
			t.Error("provider request must use POST")
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Error("provider request did not use form encoding")
		}
		if r.Header.Get("X-FusionAuth-TenantId") != "tenant-id" {
			t.Errorf("%s omitted configured FusionAuth tenant header", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != "application-id" || r.Form.Get("client_secret") != authTestSecret {
			t.Error("provider request lost its configured client credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access-token", "refresh_token": "synthetic-refresh-token", "expires_in": 300})
		case "/oauth2/introspect":
			if r.Form.Get("token") != "synthetic-access-token" || r.Form.Get("token_type_hint") != "access_token" {
				t.Error("introspection request omitted access token fields")
			}
			_ = json.NewEncoder(w).Encode(authTestClaims())
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	auth, err := newAuthWithTestProvider(authTestConfig(provider.URL), provider)
	if err != nil {
		t.Fatal(err)
	}
	var tokens tokenResponse
	if err := auth.post(context.Background(), "/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "code": {"synthetic-code"}, "code_verifier": {"synthetic-verifier"}, "redirect_uri": {auth.cfg.OAuthRedirectURI}}, &tokens); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.inspect(context.Background(), tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	if err := auth.post(context.Background(), "/oauth2/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}}, &tokens); err != nil {
		t.Fatal(err)
	}
	if seen["/oauth2/token"] != 2 || seen["/oauth2/introspect"] != 1 {
		t.Fatal("authorization, refresh or introspection request was not exercised")
	}
}
func TestAuthIntrospectionValidatesRegistrationTenantExpiryAndRequiredRole(t *testing.T) {
	tests := []struct {
		name             string
		requiredRole     string
		mutate           func(map[string]any)
		wantUnauthorized bool
	}{
		{name: "registered admin", requiredRole: "admin"},
		{name: "required role case insensitive", requiredRole: "ADMIN"},
		{name: "missing required role", requiredRole: "editor", wantUnauthorized: true},
		{name: "inactive", mutate: func(c map[string]any) { c["active"] = false }, wantUnauthorized: true},
		{name: "not registered for application", mutate: func(c map[string]any) { delete(c, "applicationId") }, wantUnauthorized: true},
		{name: "wrong application", mutate: func(c map[string]any) { c["applicationId"] = "other-application" }, wantUnauthorized: true},
		{name: "wrong tenant", mutate: func(c map[string]any) { c["tid"] = "other-tenant" }, wantUnauthorized: true},
		{name: "missing tenant", mutate: func(c map[string]any) { delete(c, "tid") }, wantUnauthorized: true},
		{name: "missing subject", mutate: func(c map[string]any) { delete(c, "sub") }, wantUnauthorized: true},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }, wantUnauthorized: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := authTestClaims()
			if tt.mutate != nil {
				tt.mutate(claims)
			}
			provider := newAuthTestProvider(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(claims)
			}))
			defer provider.Close()
			cfg := authTestConfig(provider.URL)
			cfg.RequiredRole = tt.requiredRole
			auth, err := newAuthWithTestProvider(cfg, provider)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = auth.inspect(context.Background(), "synthetic-access-token")
			if tt.wantUnauthorized {
				if !errors.Is(err, errUnauthorized) {
					t.Fatalf("expected unauthorized, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("valid registered user rejected: %v", err)
			}
		})
	}
}
