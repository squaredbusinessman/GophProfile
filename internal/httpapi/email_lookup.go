package httpapi

import (
	"net/mail"
	"strings"
)

const (
	maxLookupEmailLength   = 254
	defaultAvatarPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="
)

// validateLookupEmail проверяет email для публичного поиска avatar
func validateLookupEmail(rawEmail string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if email == "" {
		return "", validationError("Missing email", "Query parameter email is required")
	}
	if len(email) > maxLookupEmailLength {
		return "", validationError("Invalid email", "Email is too long")
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return "", validationError("Invalid email", "Email must be a single value")
	}

	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", validationError("Invalid email", "Query parameter email must contain a valid email")
	}

	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || !strings.Contains(domain, ".") {
		return "", validationError("Invalid email", "Query parameter email must contain a valid email")
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", validationError("Invalid email", "Query parameter email must contain a valid email")
	}

	return email, nil
}
