package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientCredentialsTokenSource_FirstCallFetchesToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "my-client" || r.Form.Get("client_secret") != "my-secret" {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "first-token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	getToken := ClientCredentialsTokenSource(server.URL, "my-client", "my-secret")
	token, err := getToken()
	if err != nil {
		t.Fatalf("getToken() error: %v", err)
	}
	if token != "first-token" {
		t.Errorf("token = %q, want first-token", token)
	}
}

func TestClientCredentialsTokenSource_CachesUntilExpiry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	getToken := ClientCredentialsTokenSource(server.URL, "c", "s")
	for i := 0; i < 3; i++ {
		token, err := getToken()
		if err != nil {
			t.Fatalf("getToken() #%d error: %v", i+1, err)
		}
		if token != "cached-token" {
			t.Errorf("getToken() #%d token = %q", i+1, token)
		}
	}
	if callCount != 1 {
		t.Errorf("server was called %d times, want 1 (cache used)", callCount)
	}
}

func TestClientCredentialsTokenSource_Non2xxReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	getToken := ClientCredentialsTokenSource(server.URL, "c", "s")
	_, err := getToken()
	if err == nil {
		t.Fatal("getToken() expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want to mention 401", err)
	}
}
