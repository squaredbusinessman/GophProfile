package httpapi

import (
	"net/http"
	"testing"
)

// newRouterForTest создаёт маршрутизатор и завершает тест при ошибке телеметрии
func newRouterForTest(t *testing.T, cfg RouterConfig) http.Handler {
	t.Helper()
	handler, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return handler
}
