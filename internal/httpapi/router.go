package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

var requestCounter uint64

type RouterConfig struct {
	ServiceName    string
	Version        string
	Logger         zerolog.Logger
	AvatarUploader AvatarUploader
}

type Router struct {
	serviceName    string
	version        string
	logger         zerolog.Logger
	avatarUploader AvatarUploader
	mux            *http.ServeMux
}

type AvatarUploader interface {
	UploadAvatar(ctx context.Context, req app.AvatarUploadRequest) (app.AvatarUploadResult, error)
}

// NewRouter создает HTTP router приложения
func NewRouter(cfg RouterConfig) http.Handler {
	router := &Router{
		serviceName:    cfg.ServiceName,
		version:        cfg.Version,
		logger:         cfg.Logger,
		avatarUploader: cfg.AvatarUploader,
		mux:            http.NewServeMux(),
	}

	router.mux.HandleFunc("/health", router.handleHealth)
	router.mux.HandleFunc("/api/v1/avatars", router.handleAvatars)

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

// handleAvatars обрабатывает avatar collection endpoint
func (r *Router) handleAvatars(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	r.handleAvatarUpload(w, req)
}

// handleAvatarUpload принимает multipart avatar и запускает обработку
func (r *Router) handleAvatarUpload(w http.ResponseWriter, req *http.Request) {
	if r.avatarUploader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar upload is not configured",
		})
		return
	}

	upload, err := ValidateAvatarUploadRequest(w, req)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	defer upload.Close()

	file, err := upload.Open()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid file",
			"details": "Cannot open uploaded file",
		})
		return
	}
	defer file.Close()

	body, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid file",
			"details": "Cannot read uploaded file",
		})
		return
	}

	width, height, err := imageDimensions(bytes.NewReader(body), upload.ContentType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid file format",
			"details": "Cannot determine image dimensions",
		})
		return
	}

	result, err := r.avatarUploader.UploadAvatar(req.Context(), app.AvatarUploadRequest{
		UserEmail:   upload.UserEmail,
		FileName:    upload.FileName,
		ContentType: upload.ContentType,
		Size:        int64(len(body)),
		Width:       width,
		Height:      height,
		Reader:      bytes.NewReader(body),
	})
	if err != nil {
		r.logger.Error().Err(err).Msg("avatar upload failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Avatar upload failed",
		})
		return
	}

	writeJSON(w, http.StatusCreated, AvatarUploadResponse{
		ID:        result.ID,
		UserID:    result.UserID,
		Email:     result.Email,
		URL:       "/api/v1/avatars/" + result.ID,
		Status:    string(result.Status),
		Width:     result.Width,
		Height:    result.Height,
		CreatedAt: result.CreatedAt,
	})
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Version   string            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

type AvatarUploadResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// writeValidationError записывает HTTP-ответ для ошибки валидации
func writeValidationError(w http.ResponseWriter, err error) {
	validationErr, ok := err.(*ValidationError)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request",
		})
		return
	}
	writeJSON(w, validationErr.StatusCode, validationErr)
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

// imageDimensions определяет ширину и высоту изображения
func imageDimensions(reader io.Reader, contentType string) (int, int, error) {
	if contentType == "image/webp" {
		return webpDimensions(reader)
	}

	cfg, _, err := image.DecodeConfig(reader)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// webpDimensions определяет размеры WebP по RIFF container metadata
func webpDimensions(reader io.Reader) (int, int, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, 0, err
	}
	if len(data) < 30 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, fmt.Errorf("invalid webp header")
	}

	chunk := string(data[12:16])
	switch chunk {
	case "VP8X":
		if len(data) < 30 {
			return 0, 0, fmt.Errorf("invalid webp vp8x header")
		}
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		return width, height, nil
	case "VP8 ":
		if len(data) < 30 || data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, fmt.Errorf("invalid webp vp8 header")
		}
		width := int(data[26]) | int(data[27]&0x3f)<<8
		height := int(data[28]) | int(data[29]&0x3f)<<8
		return width, height, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0, fmt.Errorf("invalid webp vp8l header")
		}
		b0 := uint32(data[21])
		b1 := uint32(data[22])
		b2 := uint32(data[23])
		b3 := uint32(data[24])
		width := int(1 + (((b1 & 0x3f) << 8) | b0))
		height := int(1 + ((b3 << 6) | (b2 << 2) | ((b1 & 0xc0) >> 6)))
		return width, height, nil
	default:
		return 0, 0, fmt.Errorf("unsupported webp chunk")
	}
}
