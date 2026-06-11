package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/squaredbusinessman/GophProfile/internal/config"
)

var ErrSecretNotFound = errors.New("vault secret not found")

type Client struct {
	addr       string
	token      string
	mount      string
	httpClient *http.Client
}

// NewClient создает Vault client для чтения секретов
func NewClient(cfg config.VaultConfig) *Client {
	return &Client{
		addr:  strings.TrimRight(cfg.Addr, "/"),
		token: cfg.Token,
		mount: strings.Trim(cfg.Mount, "/"),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ReadKV2 читает secret из Vault KV v2 engine
func (c *Client) ReadKV2(ctx context.Context, path string) (map[string]string, error) {
	secretURL, err := c.secretURL(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, secretURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read vault secret: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrSecretNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("read vault secret status: %d", resp.StatusCode)
	}

	var payload kv2Response
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode vault secret: %w", err)
	}

	return payload.Data.Data, nil
}

// HealthCheck проверяет доступность Vault API
func (c *Client) HealthCheck(ctx context.Context) error {
	healthURL := c.addr + "/v1/sys/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("create vault health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("check vault health: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == httpStatusStandby {
		return nil
	}
	return fmt.Errorf("vault health status: %d", resp.StatusCode)
}

// secretURL собирает URL для чтения секрета KV v2
func (c *Client) secretURL(path string) (string, error) {
	baseURL, err := url.Parse(c.addr)
	if err != nil {
		return "", fmt.Errorf("parse vault addr: %w", err)
	}

	secretPath := strings.Trim(path, "/")
	baseURL.Path = fmt.Sprintf("/v1/%s/data/%s", c.mount, secretPath)
	return baseURL.String(), nil
}

type kv2Response struct {
	Data struct {
		Data map[string]string `json:"data"`
	} `json:"data"`
}

const (
	httpStatusStandby = 429
)
