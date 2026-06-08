package app

import (
	"context"
	"testing"
)

// TestLoadConfigUsesEnvWhenVaultDisabled проверяет загрузку конфига без Vault
func TestLoadConfigUsesEnvWhenVaultDisabled(t *testing.T) {
	t.Setenv("VAULT_ENABLED", "false")
	t.Setenv("HTTP_ADDR", "127.0.0.1:18080")

	cfg, err := LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:18080" {
		t.Fatalf("HTTP.Addr = %q, want env value", cfg.HTTP.Addr)
	}
}
