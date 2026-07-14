// Package s3 предоставляет инструментированный клиент объектного хранилища S3
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
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

var (
	// ErrNotFound сообщает об отсутствии объекта S3
	ErrNotFound = errors.New("s3 object not found")
	// ErrInvalidKey сообщает о недопустимом ключе объекта S3
	ErrInvalidKey = errors.New("invalid s3 object key")
	// ErrInvalidConfig сообщает о недопустимой конфигурации S3
	ErrInvalidConfig = errors.New("invalid s3 config")
	// errOperationPanicked отмечает аварийное завершение запроса S3
	errOperationPanicked = errors.New("s3 operation panicked")
)

// Client выполняет операции с объектами S3 и записывает их телеметрию
type Client struct {
	bucket    string
	region    string
	api       objectStorageAPI
	telemetry s3Telemetry
	breaker   *resilience.CircuitBreaker
}

// ObjectMetadata содержит безопасные метаданные объекта S3
type ObjectMetadata struct {
	// Key содержит ключ объекта и не экспортируется в телеметрию
	Key string
	// ContentType содержит MIME-тип объекта
	ContentType string
	// Size содержит размер объекта в байтах
	Size int64
	// ETag содержит идентификатор версии объекта
	ETag string
	// LastModified содержит время последнего изменения объекта
	LastModified time.Time
	// UserMetadata содержит пользовательские метаданные объекта
	UserMetadata map[string]string
}

type objectStorageAPI interface {
	PutObject(ctx context.Context, bucket string, key string, reader io.Reader, size int64, contentType string) error
	GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error)
	StatObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error)
	RemoveObject(ctx context.Context, bucket string, key string) error
	BucketExists(ctx context.Context, bucket string) (bool, error)
	MakeBucket(ctx context.Context, bucket string, region string) error
}

type minioAdapter struct {
	sdk *minio.Client
}

// NewClient создаёт S3-совместимый клиент из конфигурации приложения
func NewClient(cfg config.S3Config, breakerCfg ...resilience.CircuitBreakerConfig) (*Client, error) {
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

	client, err := newClientWithRegion(cfg.Bucket, cfg.Region, &minioAdapter{sdk: sdk}, breakerCfg...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// newClientWithRegion создаёт клиент S3 с явно заданным регионом
func newClientWithRegion(bucket string, region string, api objectStorageAPI, breakerCfg ...resilience.CircuitBreakerConfig) (*Client, error) {
	telemetry, err := newS3Telemetry()
	if err != nil {
		return nil, fmt.Errorf("create s3 telemetry: %w", err)
	}
	cfg := resilience.CircuitBreakerConfig{}
	if len(breakerCfg) > 0 {
		cfg = breakerCfg[0]
	}
	return &Client{
		bucket:    bucket,
		region:    region,
		api:       api,
		telemetry: telemetry,
		breaker:   resilience.NewCircuitBreaker("s3", cfg),
	}, nil
}

// Put сохраняет объект в S3-compatible хранилище
func (c *Client) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	ctx, operation := c.telemetry.startS3Operation(ctx, "Put", "PutObject", objectAttributes(size, contentType)...)

	err := c.callS3(func() error {
		return c.api.PutObject(ctx, c.bucket, key, reader, size, contentType)
	})
	if err != nil {
		operation.finish(s3ResultError, err)
		return wrapError("put object", err)
	}
	operation.finish(s3ResultSuccess, nil)

	return nil
}

// Get возвращает поток объекта и его метаданные
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMetadata, error) {
	if err := validateKey(key); err != nil {
		return nil, ObjectMetadata{}, err
	}

	metadata, err := c.statObject(ctx, key)
	if err != nil {
		return nil, ObjectMetadata{}, wrapError("stat object", err)
	}

	object, err := c.getObject(ctx, key, metadata)
	if err != nil {
		return nil, ObjectMetadata{}, wrapError("get object", err)
	}

	return object, metadata, nil
}

// Delete удаляет объект и считает отсутствие объекта успешным результатом
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	ctx, operation := c.telemetry.startS3Operation(ctx, "Delete", "DeleteObject")
	err := c.callS3(func() error {
		return c.api.RemoveObject(ctx, c.bucket, key)
	})
	if err != nil {
		if isNotFound(err) {
			operation.finish(s3ResultSuccess, nil)
			return nil
		}
		operation.finish(s3ResultError, err)
		return wrapError("delete object", err)
	}
	operation.finish(s3ResultSuccess, nil)

	return nil
}

// Exists проверяет наличие объекта без чтения тела
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}

	ctx, operation := c.telemetry.startS3Operation(ctx, "Exists", "HeadObject")
	var metadata ObjectMetadata
	err := c.callS3(func() error {
		var statErr error
		metadata, statErr = c.api.StatObject(ctx, c.bucket, key)
		return statErr
	})
	if err != nil {
		if isNotFound(err) {
			operation.finish(s3ResultNotFound, nil)
			return false, nil
		}
		operation.finish(s3ResultError, err)
		return false, wrapError("stat object", err)
	}
	operation.finish(s3ResultSuccess, nil, objectAttributes(metadata.Size, metadata.ContentType)...)

	return true, nil
}

// HealthCheck проверяет доступность bucket в S3-compatible хранилище
func (c *Client) HealthCheck(ctx context.Context) error {
	var exists bool
	err := c.callS3(func() error {
		var checkErr error
		exists, checkErr = c.api.BucketExists(ctx, c.bucket)
		return checkErr
	})
	if err != nil {
		return wrapError("check bucket", err)
	}
	if !exists {
		return fmt.Errorf("%w: bucket %s", ErrNotFound, c.bucket)
	}
	return nil
}

// EnsureBucket создает bucket если он отсутствует
func (c *Client) EnsureBucket(ctx context.Context) (resultErr error) {
	ctx, operation := c.telemetry.startS3Operation(ctx, "EnsureBucket", "HeadBucket")
	defer func() {
		if resultErr != nil {
			operation.finish(s3ResultError, resultErr)
		} else {
			operation.finish(s3ResultSuccess, nil)
		}
	}()

	var exists bool
	err := c.callS3(func() error {
		var checkErr error
		exists, checkErr = c.api.BucketExists(ctx, c.bucket)
		return checkErr
	})
	if err != nil {
		return wrapError("check bucket", err)
	}
	if exists {
		return nil
	}

	if err := c.callS3(func() error {
		return c.api.MakeBucket(ctx, c.bucket, c.region)
	}); err != nil {
		exists, checkErr := false, error(nil)
		checkErr = c.callS3(func() error {
			var existsErr error
			exists, existsErr = c.api.BucketExists(ctx, c.bucket)
			return existsErr
		})
		if checkErr == nil && exists {
			return nil
		}
		return wrapError("create bucket", err)
	}
	return nil
}

// statObject получает metadata и измеряет HeadObject отдельно от GetObject
func (c *Client) statObject(ctx context.Context, key string) (ObjectMetadata, error) {
	ctx, operation := c.telemetry.startS3Operation(ctx, "Stat", "HeadObject")
	var metadata ObjectMetadata
	err := c.callS3(func() error {
		var statErr error
		metadata, statErr = c.api.StatObject(ctx, c.bucket, key)
		return statErr
	})
	if err != nil {
		if isNotFound(err) {
			operation.finish(s3ResultNotFound, nil)
		} else {
			operation.finish(s3ResultError, err)
		}
		return ObjectMetadata{}, err
	}
	operation.finish(s3ResultSuccess, nil, objectAttributes(metadata.Size, metadata.ContentType)...)
	return metadata, nil
}

// getObject открывает поток объекта без дополнительного чтения body
func (c *Client) getObject(ctx context.Context, key string, metadata ObjectMetadata) (io.ReadCloser, error) {
	ctx, operation := c.telemetry.startS3Operation(ctx, "Get", "GetObject", objectAttributes(metadata.Size, metadata.ContentType)...)
	var object io.ReadCloser
	err := c.callS3(func() error {
		var getErr error
		object, getErr = c.api.GetObject(ctx, c.bucket, key)
		return getErr
	})
	if err != nil {
		if isNotFound(err) {
			operation.finish(s3ResultNotFound, nil)
		} else {
			operation.finish(s3ResultError, err)
		}
		return nil, err
	}
	return &observedReadCloser{body: object, operation: operation}, nil
}

// Bucket возвращает имя bucket хранилища
func (c *Client) Bucket() string {
	return c.bucket
}

// callS3 выполняет S3-запрос через автоматический выключатель и не считает 404 отказом зависимости
func (c *Client) callS3(operation func() error) (err error) {
	done, err := c.breaker.Allow()
	if err != nil {
		return err
	}
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			done(errOperationPanicked)
			panic(panicValue)
		}
		if isNotFound(err) {
			done(nil)
			return
		}
		done(err)
	}()

	err = operation()
	return err
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

// MakeBucket создает bucket через MinIO SDK
func (a *minioAdapter) MakeBucket(ctx context.Context, bucket string, region string) error {
	return a.sdk.MakeBucket(ctx, bucket, minio.MakeBucketOptions{
		Region: region,
	})
}

// OriginalObjectKey возвращает object key оригинального изображения по UUID пользователя
func OriginalObjectKey(userID string, avatarID string) string {
	return joinKeyParts("avatars", objectKeySegment(userID), objectKeySegment(avatarID), "original")
}

// Thumb100ObjectKey возвращает object key миниатюры 100x100 по UUID пользователя
func Thumb100ObjectKey(userID string, avatarID string) string {
	return ThumbnailObjectKey(userID, avatarID, "100x100")
}

// Thumb300ObjectKey возвращает object key миниатюры 300x300 по UUID пользователя
func Thumb300ObjectKey(userID string, avatarID string) string {
	return ThumbnailObjectKey(userID, avatarID, "300x300")
}

// ThumbnailObjectKey возвращает object key миниатюры указанного размера по UUID пользователя
func ThumbnailObjectKey(userID string, avatarID string, size string) string {
	return joinKeyParts("avatars", objectKeySegment(userID), objectKeySegment(avatarID), objectKeySegment(size))
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
		return "", false, fmt.Errorf("%w: endpoint parse error: %w", ErrInvalidConfig, err)
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

// objectKeySegment экранирует сегмент object key для безопасного хранения
func objectKeySegment(value string) string {
	return url.PathEscape(strings.Trim(value, "/"))
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
func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return fmt.Errorf("%s: %w", operation, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
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
