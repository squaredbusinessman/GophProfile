package s3

import "testing"

// newClientWithRegionForTest создаёт тестовый S3-клиент и завершает тест при ошибке телеметрии
func newClientWithRegionForTest(t *testing.T, bucket string, region string, api objectStorageAPI) *Client {
	t.Helper()
	client, err := newClientWithRegion(bucket, region, api)
	if err != nil {
		t.Fatalf("newClientWithRegion() error = %v", err)
	}
	return client
}
