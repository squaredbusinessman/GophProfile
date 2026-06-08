package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

var (
	ErrNotFound      = errors.New("s3 object not found")
	ErrInvalidKey    = errors.New("invalid s3 object key")
	ErrInvalidConfig = errors.New("invalid s3 config")
)

type Store interface {
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectMetadata, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

type Client struct {
	bucket string
	api    objectStorageAPI
}

type ObjectMetadata struct {
	Key          string
	ContentType  string
	Size         int64
	ETag         string
	LastModified time.Time
	UserMetadata map[string]string
}

type objectStorageAPI interface {
	PutObject(ctx context.Context, bucket string, key string, reader io.Reader, size int64, contentType string) error
	GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error)
	StatObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error)
	RemoveObject(ctx context.Context, bucket string, key string) error
	BucketExists(ctx context.Context, bucket string) (bool, error)
}

type minioAdapter struct {
	sdk *minio.Client
}

// NewClient создает S3-compatible client из конфигурации приложения
func NewClient(cfg config.S3Config) (*Client, error) {
	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("%w: empty bucket", ErrInvalidConfig)
	}

	options := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	}
	if cfg.UsePathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}

	sdk, err := minio.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	return newClientWithAPI(cfg.Bucket, &minioAdapter{sdk: sdk}), nil
}

// newClientWithAPI создает S3 client поверх готового SDK-совместимого API
func newClientWithAPI(bucket string, api objectStorageAPI) *Client {
	return &Client{
		bucket: bucket,
		api:    api,
	}
}

// Put сохраняет объект в S3-compatible хранилище
func (c *Client) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	err := c.api.PutObject(ctx, c.bucket, key, reader, size, contentType)
	if err != nil {
		return wrapError("put object", key, err)
	}

	return nil
}

// Get возвращает поток объекта и его metadata
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMetadata, error) {
	if err := validateKey(key); err != nil {
		return nil, ObjectMetadata{}, err
	}

	metadata, err := c.api.StatObject(ctx, c.bucket, key)
	if err != nil {
		return nil, ObjectMetadata{}, wrapError("stat object", key, err)
	}

	object, err := c.api.GetObject(ctx, c.bucket, key)
	if err != nil {
		return nil, ObjectMetadata{}, wrapError("get object", key, err)
	}

	return object, metadata, nil
}

// Delete удаляет объект и считает отсутствие объекта успешным результатом
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	err := c.api.RemoveObject(ctx, c.bucket, key)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return wrapError("delete object", key, err)
	}

	return nil
}

// Exists проверяет наличие объекта без чтения тела
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}

	_, err := c.api.StatObject(ctx, c.bucket, key)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, wrapError("stat object", key, err)
	}

	return true, nil
}

// HealthCheck проверяет доступность bucket в S3-compatible хранилище
func (c *Client) HealthCheck(ctx context.Context) error {
	exists, err := c.api.BucketExists(ctx, c.bucket)
	if err != nil {
		return wrapError("check bucket", "", err)
	}
	if !exists {
		return fmt.Errorf("%w: bucket %s", ErrNotFound, c.bucket)
	}
	return nil
}

// Bucket возвращает имя bucket хранилища
func (c *Client) Bucket() string {
	return c.bucket
}

// PutObject сохраняет объект через MinIO SDK
func (a *minioAdapter) PutObject(ctx context.Context, bucket string, key string, reader io.Reader, size int64, contentType string) error {
	_, err := a.sdk.PutObject(ctx, bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// GetObject возвращает поток объекта через MinIO SDK
func (a *minioAdapter) GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error) {
	return a.sdk.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
}

// StatObject возвращает metadata объекта через MinIO SDK
func (a *minioAdapter) StatObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error) {
	info, err := a.sdk.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectMetadata{}, err
	}
	return objectMetadata(key, info), nil
}

// RemoveObject удаляет объект через MinIO SDK
func (a *minioAdapter) RemoveObject(ctx context.Context, bucket string, key string) error {
	return a.sdk.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

// BucketExists проверяет наличие bucket через MinIO SDK
func (a *minioAdapter) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return a.sdk.BucketExists(ctx, bucket)
}

// OriginalObjectKey возвращает object key оригинального изображения
func OriginalObjectKey(userID string, avatarID string) string {
	return joinKeyParts("avatars", userID, avatarID, "original")
}

// Thumb100ObjectKey возвращает object key миниатюры 100x100
func Thumb100ObjectKey(userID string, avatarID string) string {
	return ThumbnailObjectKey(userID, avatarID, "100x100")
}

// Thumb300ObjectKey возвращает object key миниатюры 300x300
func Thumb300ObjectKey(userID string, avatarID string) string {
	return ThumbnailObjectKey(userID, avatarID, "300x300")
}

// ThumbnailObjectKey возвращает object key миниатюры указанного размера
func ThumbnailObjectKey(userID string, avatarID string, size string) string {
	return joinKeyParts("avatars", userID, avatarID, size)
}

// parseEndpoint разбирает endpoint в формат MinIO SDK
func parseEndpoint(rawEndpoint string) (string, bool, error) {
	rawEndpoint = strings.TrimSpace(rawEndpoint)
	if rawEndpoint == "" {
		return "", false, fmt.Errorf("%w: empty endpoint", ErrInvalidConfig)
	}
	if !strings.Contains(rawEndpoint, "://") {
		return strings.TrimRight(rawEndpoint, "/"), false, nil
	}

	parsed, err := url.Parse(rawEndpoint)
	if err != nil {
		return "", false, fmt.Errorf("%w: endpoint parse error: %v", ErrInvalidConfig, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, fmt.Errorf("%w: unsupported endpoint scheme %s", ErrInvalidConfig, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("%w: empty endpoint host", ErrInvalidConfig)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false, fmt.Errorf("%w: endpoint path is not supported", ErrInvalidConfig)
	}

	return parsed.Host, parsed.Scheme == "https", nil
}

// validateKey проверяет object key перед обращением к хранилищу
func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("%w: key must be relative", ErrInvalidKey)
	}
	return nil
}

// joinKeyParts собирает object key из сегментов пути
func joinKeyParts(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

// objectMetadata конвертирует SDK metadata в доменную metadata
func objectMetadata(key string, info minio.ObjectInfo) ObjectMetadata {
	userMetadata := make(map[string]string, len(info.UserMetadata))
	for name, value := range info.UserMetadata {
		userMetadata[name] = value
	}

	return ObjectMetadata{
		Key:          key,
		ContentType:  info.ContentType,
		Size:         info.Size,
		ETag:         info.ETag,
		LastModified: info.LastModified,
		UserMetadata: userMetadata,
	}
}

// wrapError мапит ошибку SDK в ошибку S3-слоя
func wrapError(operation string, key string, err error) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return fmt.Errorf("%s %s: %w", operation, key, ErrNotFound)
	}
	return fmt.Errorf("%s %s: %w", operation, key, err)
}

// isNotFound определяет отсутствие объекта или bucket по SDK-ошибке
func isNotFound(err error) bool {
	response := minio.ToErrorResponse(err)
	if response.Code == "" && response.StatusCode == 0 {
		return false
	}

	switch response.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return true
	default:
		return response.StatusCode == http.StatusNotFound
	}
}
