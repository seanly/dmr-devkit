// Package auth provides OAuth2 authentication helpers, including a client credentials
// token source with automatic caching and thread-safe refresh.
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const refreshBuffer = 1 * time.Minute

// ClientCredentialsTokenSource returns a function that yields Bearer tokens using the
// OAuth2 client_credentials flow. The returned function is safe for concurrent use and
// caches the token in memory until it expires (with a 1-minute buffer).
func ClientCredentialsTokenSource(tokenURL, clientID, clientSecret string) func() (string, error) {
	var mu sync.Mutex
	var cachedToken string
	var expiresAt time.Time

	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()

		if cachedToken != "" && time.Now().Add(refreshBuffer).Before(expiresAt) {
			return cachedToken, nil
		}

		token, exp, err := fetchClientCredentialsToken(tokenURL, clientID, clientSecret)
		if err != nil {
			return "", err
		}
		cachedToken = token
		expiresAt = exp
		return token, nil
	}
}

func fetchClientCredentialsToken(tokenURL, clientID, clientSecret string) (token string, expiresAt time.Time, err error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("client_credentials request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("client_credentials request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("client_credentials read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("client_credentials failed: %d %s", resp.StatusCode, string(body))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("client_credentials parse: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("client_credentials: no access_token in response")
	}

	exp := time.Time{}
	if out.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return out.AccessToken, exp, nil
}
