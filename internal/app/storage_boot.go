package app

import (
	"context"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/config"
)

const localS3BucketEnsureTimeout = 30 * time.Second

type S3BucketEnsurer interface {
	EnsureBucket(ctx context.Context) error
	Bucket() string
}

// EnsureLocalS3Bucket создает S3 bucket при локальном запуске приложения
func EnsureLocalS3Bucket(ctx context.Context, cfg config.Config, storage S3BucketEnsurer) error {
	if cfg.Env != "local" {
		return nil
	}

	ensureCtx, cancel := context.WithTimeout(ctx, localS3BucketEnsureTimeout)
	defer cancel()

	var lastErr error
	for {
		if err := storage.EnsureBucket(ensureCtx); err != nil {
			lastErr = err
		} else {
			return nil
		}

		timer := time.NewTimer(time.Second)
		select {
		case <-ensureCtx.Done():
			timer.Stop()
			return fmt.Errorf("ensure local s3 bucket %s: %w", storage.Bucket(), lastErr)
		case <-timer.C:
		}
	}
}
