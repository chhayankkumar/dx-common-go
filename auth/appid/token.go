package appid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokenSource fetches and caches a Keycloak service-account token via the
// client-credentials grant. The token authenticates this service to the
// controlplane's gRPC ServiceAuthInterceptor.
type tokenSource struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newTokenSource(cfg Config) *tokenSource {
	return &tokenSource{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Token returns a valid bearer token, refreshing when within 30s of expiry.
func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Now().Add(30*time.Second).Before(t.expiresAt) {
		return t.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {t.cfg.ClientID},
		"client_secret": {t.cfg.ClientSecret},
		"scope":         {t.cfg.scope()},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("appid token: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("appid token: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("appid token: keycloak returned %d", resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("appid token: decode response: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("appid token: empty access_token")
	}

	t.token = body.AccessToken
	t.expiresAt = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	return t.token, nil
}
