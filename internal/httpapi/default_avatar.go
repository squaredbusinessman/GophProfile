package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

const (
	defaultAvatarContentType  = "image/png"
	defaultAvatarCacheControl = "max-age=300"
	fallbackDefaultAvatarPNG  = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="
)

type DefaultAvatar struct {
	Body         []byte
	ContentType  string
	ETag         string
	CacheControl string
}

// LoadDefaultAvatar читает PNG-заглушку avatar из файла
func LoadDefaultAvatar(path string) (DefaultAvatar, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return DefaultAvatar{}, nil
	}

	body, err := os.ReadFile(normalizedPath)
	if err != nil {
		return DefaultAvatar{}, fmt.Errorf("read default avatar: %w", err)
	}

	return NewDefaultAvatar(body), nil
}

// NewDefaultAvatar создает описание статической PNG-заглушки avatar
func NewDefaultAvatar(body []byte) DefaultAvatar {
	copiedBody := append([]byte(nil), body...)
	hash := sha256.Sum256(copiedBody)

	return DefaultAvatar{
		Body:         copiedBody,
		ContentType:  defaultAvatarContentType,
		ETag:         "default-avatar-" + hex.EncodeToString(hash[:])[:16],
		CacheControl: defaultAvatarCacheControl,
	}
}

// normalizeDefaultAvatar подставляет резервную заглушку если файл не настроен
func normalizeDefaultAvatar(item DefaultAvatar) DefaultAvatar {
	if len(item.Body) == 0 {
		body, err := base64.StdEncoding.DecodeString(fallbackDefaultAvatarPNG)
		if err != nil {
			return DefaultAvatar{}
		}
		item = NewDefaultAvatar(body)
	}
	if item.ContentType == "" {
		item.ContentType = defaultAvatarContentType
	}
	if item.CacheControl == "" {
		item.CacheControl = defaultAvatarCacheControl
	}
	if item.ETag == "" {
		item = NewDefaultAvatar(item.Body)
	}
	return item
}
