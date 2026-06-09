package avatar

import (
	"errors"
	"testing"
)

// TestValidateStatusAcceptsKnownStatuses проверяет допустимые статусы avatar
func TestValidateStatusAcceptsKnownStatuses(t *testing.T) {
	statuses := []Status{
		StatusProcessing,
		StatusReady,
		StatusFailed,
		StatusDeleting,
		StatusDeleted,
	}

	for _, status := range statuses {
		if err := ValidateStatus(status); err != nil {
			t.Fatalf("ValidateStatus(%q) returned error: %v", status, err)
		}
	}
}

// TestValidateStatusRejectsUnknownStatus проверяет отказ для неизвестного статуса
func TestValidateStatusRejectsUnknownStatus(t *testing.T) {
	err := ValidateStatus(Status("unknown"))
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("error = %v, want ErrInvalidStatus", err)
	}
}

// TestObjectKeysReturnsKnownKeys проверяет сбор object keys avatar
func TestObjectKeysReturnsKnownKeys(t *testing.T) {
	thumb100 := "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar/100x100"
	item := Avatar{
		OriginalObjectKey: "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar/original",
		Thumb100ObjectKey: &thumb100,
	}

	keys := item.ObjectKeys()
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
	if keys[0] != "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar/original" || keys[1] != thumb100 {
		t.Fatalf("keys = %#v, want original and thumb100", keys)
	}
}
