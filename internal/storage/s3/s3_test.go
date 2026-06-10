package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// TestObjectKeyBuilders проверяет формат S3 object keys
func TestObjectKeyBuilders(t *testing.T) {
	userID := "/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/"
	avatarID := "/avatar-1/"

	tests := map[string]string{
		"original": OriginalObjectKey(userID, avatarID),
		"100x100":  Thumb100ObjectKey(userID, avatarID),
		"300x300":  Thumb300ObjectKey(userID, avatarID),
	}

	if tests["original"] != "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar-1/original" {
		t.Fatalf("original key = %q", tests["original"])
	}
	if tests["100x100"] != "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar-1/100x100" {
		t.Fatalf("100x100 key = %q", tests["100x100"])
	}
	if tests["300x300"] != "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar-1/300x300" {
		t.Fatalf("300x300 key = %q", tests["300x300"])
	}
}

// TestObjectKeyBuildersEscapeUnsafeSegments проверяет экранирование path-сегментов
func TestObjectKeyBuildersEscapeUnsafeSegments(t *testing.T) {
	key := OriginalObjectKey("user/id", "avatar/1")
	if key != "avatars/user%2Fid/avatar%2F1/original" {
		t.Fatalf("key = %q, want escaped segments", key)
	}
}

// TestPutDelegatesToObjectStorageAPI проверяет сохранение объекта через adapter
func TestPutDelegatesToObjectStorageAPI(t *testing.T) {
	api := &fakeObjectStorageAPI{}
	client := newClientWithAPI("avatars", api)

	err := client.Put(context.Background(), "avatars/user/avatar/original", strings.NewReader("image"), 5, "image/jpeg")
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	if api.putBucket != "avatars" {
		t.Fatalf("putBucket = %q, want avatars", api.putBucket)
	}
	if api.putKey != "avatars/user/avatar/original" {
		t.Fatalf("putKey = %q, want object key", api.putKey)
	}
	if api.putSize != 5 {
		t.Fatalf("putSize = %d, want 5", api.putSize)
	}
	if api.putContentType != "image/jpeg" {
		t.Fatalf("putContentType = %q, want image/jpeg", api.putContentType)
	}
}

// TestGetReturnsStreamAndMetadata проверяет чтение объекта и metadata
func TestGetReturnsStreamAndMetadata(t *testing.T) {
	lastModified := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	api := &fakeObjectStorageAPI{
		getBody: io.NopCloser(strings.NewReader("image")),
		statMetadata: ObjectMetadata{
			Key:          "avatars/user/avatar/original",
			ContentType:  "image/png",
			Size:         5,
			ETag:         "etag",
			LastModified: lastModified,
		},
	}
	client := newClientWithAPI("avatars", api)

	body, metadata, err := client.Get(context.Background(), "avatars/user/avatar/original")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "image" {
		t.Fatalf("body = %q, want image", string(data))
	}
	if metadata.ContentType != "image/png" || metadata.Size != 5 || metadata.LastModified != lastModified {
		t.Fatalf("metadata = %#v, want expected values", metadata)
	}
}

// TestDeleteIgnoresNotFound проверяет идемпотентность удаления отсутствующего объекта
func TestDeleteIgnoresNotFound(t *testing.T) {
	api := &fakeObjectStorageAPI{
		removeErr: minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound},
	}
	client := newClientWithAPI("avatars", api)

	err := client.Delete(context.Background(), "avatars/user/avatar/original")
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
}

// TestExistsReturnsFalseForNotFound проверяет отсутствие объекта без ошибки
func TestExistsReturnsFalseForNotFound(t *testing.T) {
	api := &fakeObjectStorageAPI{
		statErr: minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound},
	}
	client := newClientWithAPI("avatars", api)

	exists, err := client.Exists(context.Background(), "avatars/user/avatar/original")
	if err != nil {
		t.Fatalf("Exists returned error: %v", err)
	}
	if exists {
		t.Fatal("exists = true, want false")
	}
}

// TestEnsureBucketSkipsExistingBucket проверяет отсутствие создания существующего bucket
func TestEnsureBucketSkipsExistingBucket(t *testing.T) {
	api := &fakeObjectStorageAPI{
		bucketExists: true,
	}
	client := newClientWithRegion("avatars", "us-east-1", api)

	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket returned error: %v", err)
	}
	if api.makeBucket != "" {
		t.Fatalf("makeBucket = %q, want empty", api.makeBucket)
	}
}

// TestEnsureBucketCreatesMissingBucket проверяет создание отсутствующего bucket
func TestEnsureBucketCreatesMissingBucket(t *testing.T) {
	api := &fakeObjectStorageAPI{}
	client := newClientWithRegion("avatars", "us-east-1", api)

	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket returned error: %v", err)
	}
	if api.makeBucket != "avatars" {
		t.Fatalf("makeBucket = %q, want avatars", api.makeBucket)
	}
	if api.makeRegion != "us-east-1" {
		t.Fatalf("makeRegion = %q, want us-east-1", api.makeRegion)
	}
}

// TestGetMapsNotFoundToDomainError проверяет маппинг S3 not found в доменную ошибку
func TestGetMapsNotFoundToDomainError(t *testing.T) {
	api := &fakeObjectStorageAPI{
		statErr: minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound},
	}
	client := newClientWithAPI("avatars", api)

	_, _, err := client.Get(context.Background(), "avatars/user/avatar/original")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestInvalidKeyRejectsAbsolutePath проверяет отказ для абсолютного object key
func TestInvalidKeyRejectsAbsolutePath(t *testing.T) {
	api := &fakeObjectStorageAPI{}
	client := newClientWithAPI("avatars", api)

	err := client.Put(context.Background(), "/avatars/user/avatar/original", strings.NewReader("image"), 5, "image/jpeg")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("error = %v, want ErrInvalidKey", err)
	}
	if api.putKey != "" {
		t.Fatalf("putKey = %q, want empty because API should not be called", api.putKey)
	}
}

// TestParseEndpoint проверяет поддержку endpoint со схемой и без схемы
func TestParseEndpoint(t *testing.T) {
	endpoint, secure, err := parseEndpoint("http://localhost:9000")
	if err != nil {
		t.Fatalf("parseEndpoint returned error: %v", err)
	}
	if endpoint != "localhost:9000" || secure {
		t.Fatalf("endpoint = %q secure = %t, want localhost:9000 false", endpoint, secure)
	}

	endpoint, secure, err = parseEndpoint("s3.example.com")
	if err != nil {
		t.Fatalf("parseEndpoint returned error: %v", err)
	}
	if endpoint != "s3.example.com" || secure {
		t.Fatalf("endpoint = %q secure = %t, want s3.example.com false", endpoint, secure)
	}
}

type fakeObjectStorageAPI struct {
	putBucket      string
	putKey         string
	putSize        int64
	putContentType string
	putErr         error
	getBody        io.ReadCloser
	getErr         error
	statMetadata   ObjectMetadata
	statErr        error
	removeErr      error
	bucketExists   bool
	bucketErr      error
	makeBucket     string
	makeRegion     string
	makeErr        error
}

// PutObject записывает параметры сохранения в fake API
func (f *fakeObjectStorageAPI) PutObject(ctx context.Context, bucket string, key string, reader io.Reader, size int64, contentType string) error {
	f.putBucket = bucket
	f.putKey = key
	f.putSize = size
	f.putContentType = contentType
	return f.putErr
}

// GetObject возвращает fake-поток объекта
func (f *fakeObjectStorageAPI) GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getBody, nil
}

// StatObject возвращает fake metadata объекта
func (f *fakeObjectStorageAPI) StatObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error) {
	if f.statErr != nil {
		return ObjectMetadata{}, f.statErr
	}
	return f.statMetadata, nil
}

// RemoveObject возвращает fake-результат удаления объекта
func (f *fakeObjectStorageAPI) RemoveObject(ctx context.Context, bucket string, key string) error {
	return f.removeErr
}

// BucketExists возвращает fake-результат проверки bucket
func (f *fakeObjectStorageAPI) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return f.bucketExists, f.bucketErr
}

// MakeBucket записывает параметры создания bucket в fake API
func (f *fakeObjectStorageAPI) MakeBucket(ctx context.Context, bucket string, region string) error {
	f.makeBucket = bucket
	f.makeRegion = region
	return f.makeErr
}
