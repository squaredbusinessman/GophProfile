package vault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// TestReadKV2ReadsSecretData проверяет чтение секрета KV v2
func TestReadKV2ReadsSecretData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/secret/data/gophprofile" {
			t.Fatalf("path = %q, want KV v2 path", req.URL.Path)
		}
		if token := req.Header.Get("X-Vault-Token"); token != "test-token" {
			t.Fatalf("X-Vault-Token = %q, want test-token", token)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"DATABASE_URL":"postgres://example"}}}`))
	}))
	defer server.Close()

	client := NewClient(config.VaultConfig{
		Addr:    server.URL,
		Token:   "test-token",
		Mount:   "secret",
		Timeout: time.Second,
	})

	secrets, err := client.ReadKV2(context.Background(), "gophprofile")
	if err != nil {
		t.Fatalf("ReadKV2 returned error: %v", err)
	}
	if secrets["DATABASE_URL"] != "postgres://example" {
		t.Fatalf("DATABASE_URL = %q, want postgres://example", secrets["DATABASE_URL"])
	}
}

// TestReadKV2ReturnsNotFound проверяет отсутствие секрета в Vault
func TestReadKV2ReturnsNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client := NewClient(config.VaultConfig{
		Addr:    server.URL,
		Token:   "test-token",
		Mount:   "secret",
		Timeout: time.Second,
	})

	_, err := client.ReadKV2(context.Background(), "missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("error = %v, want ErrSecretNotFound", err)
	}
}

// TestHealthCheckAcceptsActiveVault проверяет healthcheck активного Vault
func TestHealthCheckAcceptsActiveVault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/sys/health" {
			t.Fatalf("path = %q, want health path", req.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(config.VaultConfig{
		Addr:    server.URL,
		Timeout: time.Second,
	})

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck returned error: %v", err)
	}
}
