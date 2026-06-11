package app

import (
	"context"
	"fmt"

	"github.com/squaredbusinessman/GophProfile/internal/config"
	"github.com/squaredbusinessman/GophProfile/internal/secrets/vault"
)

// LoadConfig читает конфигурацию и применяет секреты из Vault при включенном режиме
func LoadConfig(ctx context.Context) (config.Config, error) {
	cfg := config.Load()
	if !cfg.Vault.Enabled {
		return cfg, nil
	}

	client := vault.NewClient(cfg.Vault)
	secrets, err := client.ReadKV2(ctx, cfg.Vault.ServicePath)
	if err != nil {
		return config.Config{}, fmt.Errorf("load vault secrets: %w", err)
	}

	cfg.ApplySecrets(secrets)
	return cfg, nil
}
