package app

import "testing"

// newAvatarUploadServiceForTest создаёт сервис загрузки и завершает тест при ошибке телеметрии
func newAvatarUploadServiceForTest(t *testing.T, users UserLookup, avatarOutbox AvatarOutboxStore, objects ObjectStore, publisher EventPublisher) *AvatarUploadService {
	t.Helper()
	service, err := NewAvatarUploadService(users, avatarOutbox, objects, publisher)
	if err != nil {
		t.Fatalf("NewAvatarUploadService() error = %v", err)
	}
	return service
}

// newAvatarProcessServiceForTest создаёт сервис обработки и завершает тест при ошибке телеметрии
func newAvatarProcessServiceForTest(t *testing.T, avatars AvatarMetadataStore, objects AvatarObjectStore, producer EventPublisher) *AvatarProcessService {
	t.Helper()
	service, err := NewAvatarProcessService(avatars, objects, producer)
	if err != nil {
		t.Fatalf("NewAvatarProcessService() error = %v", err)
	}
	return service
}

// newAvatarDeleteServiceForTest создаёт сервис удаления и завершает тест при ошибке телеметрии
func newAvatarDeleteServiceForTest(t *testing.T, avatars AvatarDeleteRepository, outbox AvatarDeleteOutboxStore, producer EventPublisher) *AvatarDeleteService {
	t.Helper()
	service, err := NewAvatarDeleteService(avatars, outbox, producer)
	if err != nil {
		t.Fatalf("NewAvatarDeleteService() error = %v", err)
	}
	return service
}

// newAvatarDeleteWorkerServiceForTest создаёт фоновый сервис удаления и завершает тест при ошибке телеметрии
func newAvatarDeleteWorkerServiceForTest(t *testing.T, avatars AvatarDeleteWorkerRepository, objects AvatarDeleteObjectStore) *AvatarDeleteWorkerService {
	t.Helper()
	service, err := NewAvatarDeleteWorkerService(avatars, objects)
	if err != nil {
		t.Fatalf("NewAvatarDeleteWorkerService() error = %v", err)
	}
	return service
}

// newOutboxPublisherServiceForTest создаёт сервис публикации outbox и завершает тест при ошибке телеметрии
func newOutboxPublisherServiceForTest(t *testing.T, store OutboxEventStore, publisher EventPublisher) *OutboxPublisherService {
	t.Helper()
	service, err := NewOutboxPublisherService(store, publisher)
	if err != nil {
		t.Fatalf("NewOutboxPublisherService() error = %v", err)
	}
	return service
}
