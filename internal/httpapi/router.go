package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

var requestCounter uint64

type RouterConfig struct {
	ServiceName string
	Version     string
	Logger      zerolog.Logger
}

type Router struct {
	serviceName string
	version     string
	logger      zerolog.Logger
	mux         *http.ServeMux
}

// NewRouter создает HTTP router приложения
func NewRouter(cfg RouterConfig) http.Handler {
	router := &Router{
		serviceName: cfg.ServiceName,
		version:     cfg.Version,
		logger:      cfg.Logger,
		mux:         http.NewServeMux(),
	}

	router.mux.HandleFunc("/health", router.handleHealth)

	return router
}

// ServeHTTP обрабатывает HTTP-запрос и пишет access log с корректным уровнем
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	startedAt := time.Now()
	requestID := requestID(req)
	w.Header().Set("X-Request-ID", requestID)

	rec := &statusRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	r.mux.ServeHTTP(rec, req)

	event := r.logger.Info()
	if rec.statusCode >= http.StatusInternalServerError {
		event = r.logger.Error()
	} else if rec.statusCode >= http.StatusBadRequest {
		event = r.logger.Warn()
	}

	event.
		Str("request_id", requestID).
		Str("http_method", req.Method).
		Str("http_path", req.URL.Path).
		Str("remote_addr", req.RemoteAddr).
		Int("http_status", rec.statusCode).
		Int("response_bytes", rec.bytesWritten).
		Dur("duration", time.Since(startedAt)).
		Msg("http request completed")
}

// handleHealth возвращает базовый healthcheck без внешних зависимостей
func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		Service:   r.serviceName,
		Version:   r.version,
		Timestamp: time.Now().UTC(),
		Checks:    map[string]string{},
	})
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Version   string            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

// writeJSON записывает JSON-ответ с указанным HTTP-статусом
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

// WriteHeader сохраняет HTTP-статус перед отправкой ответа клиенту
func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write сохраняет размер отправленного ответа
func (r *statusRecorder) Write(data []byte) (int, error) {
	written, err := r.ResponseWriter.Write(data)
	r.bytesWritten += written
	return written, err
}

// requestID возвращает request id из заголовка или генерирует новый
func requestID(req *http.Request) string {
	if requestID := req.Header.Get("X-Request-ID"); requestID != "" {
		return requestID
	}

	next := atomic.AddUint64(&requestCounter, 1)
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), strconv.FormatUint(next, 36))
}
