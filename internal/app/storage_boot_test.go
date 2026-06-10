package app

import (
	"context"
	"testing"

	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// TestEnsureLocalS3BucketSkipsNonLocalEnv проверяет пропуск bucket setup вне local env
func TestEnsureLocalS3BucketSkipsNonLocalEnv(t *testing.T) {
	storage := &fakeS3BucketEnsurer{}

	err := EnsureLocalS3Bucket(context.Background(), config.Config{Env: "prod"}, storage)
	if err != nil {
		t.Fatalf("EnsureLocalS3Bucket returned error: %v", err)
	}
	if storage.calls != 0 {
		t.Fatalf("calls = %d, want 0", storage.calls)
	}
}

// TestEnsureLocalS3BucketCreatesBucketInLocalEnv проверяет создание bucket в local env
func TestEnsureLocalS3BucketCreatesBucketInLocalEnv(t *testing.T) {
	storage := &fakeS3BucketEnsurer{bucket: "avatars"}

	err := EnsureLocalS3Bucket(context.Background(), config.Config{Env: "local"}, storage)
	if err != nil {
		t.Fatalf("EnsureLocalS3Bucket returned error: %v", err)
	}
	if storage.calls != 1 {
		t.Fatalf("calls = %d, want 1", storage.calls)
	}
}

type fakeS3BucketEnsurer struct {
	bucket string
	calls  int
}

// EnsureBucket записывает вызов fake bucket setup
func (f *fakeS3BucketEnsurer) EnsureBucket(ctx context.Context) error {
	f.calls++
	return nil
}

// Bucket возвращает fake bucket name
func (f *fakeS3BucketEnsurer) Bucket() string {
	return f.bucket
}
