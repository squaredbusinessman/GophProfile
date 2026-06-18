package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
)

var requestCounter uint64

type RouterConfig struct {
	ServiceName    string
	Version        string
	Logger         zerolog.Logger
	AllowedOrigins []string
	RateLimitRPS   int
	RateLimitBurst int
	HealthChecks   map[string]HealthCheck
	DefaultAvatar  DefaultAvatar
	UserResolver   UserResolver
	AvatarUploader AvatarUploader
	AvatarReader   AvatarReader
	AvatarDeleter  AvatarDeleter
}

type Router struct {
	serviceName    string
	version        string
	logger         zerolog.Logger
	cors           corsPolicy
	rateLimiter    *clientRateLimiter
	healthChecks   map[string]HealthCheck
	defaultAvatar  DefaultAvatar
	userResolver   UserResolver
	avatarUploader AvatarUploader
	avatarReader   AvatarReader
	avatarDeleter  AvatarDeleter
	mux            *http.ServeMux
}

type HealthCheck func(ctx context.Context) error

type UserResolver interface {
	ResolveUserByEmail(ctx context.Context, email string) (app.UserResolveResult, error)
}

type AvatarUploader interface {
	UploadAvatar(ctx context.Context, req app.AvatarUploadRequest) (app.AvatarUploadResult, error)
}

type AvatarReader interface {
	GetAvatarByID(ctx context.Context, avatarID string, size string, format string) (app.AvatarReadResult, error)
	GetLatestAvatarByUserID(ctx context.Context, userID string, size string, format string) (app.AvatarReadResult, error)
	GetLatestAvatarByEmail(ctx context.Context, email string, size string, format string) (app.AvatarReadResult, error)
	GetAvatarMetadata(ctx context.Context, avatarID string) (app.AvatarMetadataResult, error)
	ListAvatarsByUserID(ctx context.Context, userID string, limit int, offset int) (app.AvatarListResult, error)
}

type AvatarDeleter interface {
	DeleteAvatarByID(ctx context.Context, avatarID string, requesterUserID string) error
	DeleteLatestAvatarByUserID(ctx context.Context, targetUserID string, requesterUserID string) error
}

// NewRouter создает HTTP router приложения
func NewRouter(cfg RouterConfig) http.Handler {
	router := &Router{
		serviceName:    cfg.ServiceName,
		version:        cfg.Version,
		logger:         cfg.Logger,
		cors:           newCORSPolicy(cfg.AllowedOrigins),
		rateLimiter:    newClientRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		healthChecks:   cfg.HealthChecks,
		defaultAvatar:  normalizeDefaultAvatar(cfg.DefaultAvatar),
		userResolver:   cfg.UserResolver,
		avatarUploader: cfg.AvatarUploader,
		avatarReader:   cfg.AvatarReader,
		avatarDeleter:  cfg.AvatarDeleter,
		mux:            http.NewServeMux(),
	}

	router.mux.HandleFunc("/health", router.handleHealth)
	router.mux.HandleFunc("/api/v1/avatar", router.handlePublicAvatarByEmail)
	router.mux.HandleFunc("/api/v1/avatars", router.handleAvatars)
	router.mux.HandleFunc("/api/v1/avatars/", router.handleAvatarByID)
	router.mux.HandleFunc("/api/v1/users/resolve", router.handleUserResolve)
	router.mux.HandleFunc("/api/v1/users/", router.handleUsers)

	return router
}

// ServeHTTP обрабатывает HTTP-запрос и пишет access log с корректным уровнем
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	startedAt := time.Now()
	requestID := requestID(req)
	w.Header().Set("X-Request-ID", requestID)
	requestCtx := app.ContextWithLogger(req.Context(), r.logger)
	requestCtx = app.ContextWithRequestID(requestCtx, requestID)
	req = req.WithContext(requestCtx)

	rec := &statusRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	if !r.handleCORS(rec, req) {
		if r.shouldLimit(req) && !r.allowRequest(req) {
			rec.Header().Set("Retry-After", "1")
			writeJSON(rec, http.StatusTooManyRequests, map[string]string{
				"error": "Too many requests",
			})
		} else {
			r.mux.ServeHTTP(rec, req)
		}
	}

	logger := app.LoggerFromContext(req.Context())
	event := logger.Info()
	if rec.statusCode >= http.StatusInternalServerError {
		event = logger.Error()
	} else if rec.statusCode >= http.StatusBadRequest {
		event = logger.Warn()
	}

	event.
		Str("http_method", req.Method).
		Str("http_path", req.URL.Path).
		Str("remote_addr", req.RemoteAddr).
		Int("http_status", rec.statusCode).
		Int("response_bytes", rec.bytesWritten).
		Dur("duration", time.Since(startedAt)).
		Msg("http request completed")
}

const healthCheckTimeout = 2 * time.Second

// handleHealth возвращает healthcheck приложения и внешних зависимостей
func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	status := "ok"
	statusCode := http.StatusOK
	checks := make(map[string]string, len(r.healthChecks))

	for name, check := range r.healthChecks {
		checkCtx, cancel := context.WithTimeout(req.Context(), healthCheckTimeout)
		err := check(checkCtx)
		cancel()

		if err != nil {
			checks[name] = "error"
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
			continue
		}
		checks[name] = "ok"
	}

	writeJSON(w, statusCode, HealthResponse{
		Status:    status,
		Service:   r.serviceName,
		Version:   r.version,
		Timestamp: time.Now().UTC(),
		Checks:    checks,
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

// handlePublicAvatarByEmail возвращает avatar через публичный lookup по email
func (r *Router) handlePublicAvatarByEmail(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}
	if r.avatarReader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar reader is not configured",
		})
		return
	}

	email, err := validateLookupEmail(req.URL.Query().Get("email"))
	if err != nil {
		writeValidationError(w, err)
		return
	}

	result, err := r.avatarReaderResult(req, func(ctx context.Context, size string, format string) (app.AvatarReadResult, error) {
		return r.avatarReader.GetLatestAvatarByEmail(ctx, email, size, format)
	})
	if err != nil {
		if errors.Is(err, app.ErrAvatarNotFound) {
			if err := ensureDefaultAvatarRequest(req); err != nil {
				writeAvatarReadError(w, err)
				return
			}
			writeDefaultAvatar(w, r.defaultAvatar)
			return
		}
		if isInternalAvatarReadError(err) {
			r.logInternalError(req.Context(), err, "public avatar read failed")
		}
		writeAvatarReadError(w, err)
		return
	}
	defer func() {
		_ = result.Body.Close()
	}()

	writeAvatarBinary(w, result)
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
	defer func() {
		_ = upload.Close()
	}()

	file, err := upload.Open()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid file",
			"details": "Cannot open uploaded file",
		})
		return
	}
	defer func() {
		_ = file.Close()
	}()

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
		UserID:      upload.UserID,
		FileName:    upload.FileName,
		ContentType: upload.ContentType,
		Size:        int64(len(body)),
		Width:       width,
		Height:      height,
		Reader:      bytes.NewReader(body),
	})
	if err != nil {
		if !errors.Is(err, app.ErrUserNotFound) {
			r.logInternalError(req.Context(), err, "avatar upload failed")
		}
		writeAvatarUploadError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, AvatarUploadResponse{
		ID:        result.ID,
		UserID:    result.UserID,
		URL:       "/api/v1/avatars/" + result.ID,
		Status:    string(result.Status),
		Width:     result.Width,
		Height:    result.Height,
		CreatedAt: result.CreatedAt,
	})
}

// handleAvatarByID обрабатывает GET avatar по avatar id
func (r *Router) handleAvatarByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodDelete {
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodDelete}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	avatarPath := strings.TrimPrefix(req.URL.Path, "/api/v1/avatars/")
	if strings.HasSuffix(avatarPath, "/metadata") {
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
			return
		}
		avatarID := strings.TrimSuffix(avatarPath, "/metadata")
		if avatarID == "" || strings.Contains(avatarID, "/") {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "Avatar not found",
			})
			return
		}
		r.handleAvatarMetadata(w, req, avatarID)
		return
	}

	avatarID := avatarPath
	if avatarID == "" || strings.Contains(avatarID, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Avatar not found",
		})
		return
	}

	if req.Method == http.MethodDelete {
		r.handleAvatarDeleteByID(w, req, avatarID)
		return
	}

	result, err := r.avatarReaderResult(req, func(ctx context.Context, size string, format string) (app.AvatarReadResult, error) {
		return r.avatarReader.GetAvatarByID(ctx, avatarID, size, format)
	})
	if err != nil {
		if isInternalAvatarReadError(err) {
			r.logInternalError(req.Context(), err, "avatar read failed")
		}
		writeAvatarReadError(w, err)
		return
	}
	defer func() {
		_ = result.Body.Close()
	}()

	writeAvatarBinary(w, result)
}

// handleAvatarDeleteByID удаляет avatar по id
func (r *Router) handleAvatarDeleteByID(w http.ResponseWriter, req *http.Request, avatarID string) {
	if r.avatarDeleter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar delete is not configured",
		})
		return
	}

	requesterUserID, err := validateRequesterUserID(req.Header.Get("X-User-ID"))
	if err != nil {
		writeValidationError(w, err)
		return
	}

	if err := r.avatarDeleter.DeleteAvatarByID(req.Context(), avatarID, requesterUserID); err != nil {
		if isInternalAvatarDeleteError(err) {
			r.logInternalError(req.Context(), err, "avatar delete failed")
		}
		writeAvatarDeleteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAvatarMetadata возвращает JSON metadata avatar
func (r *Router) handleAvatarMetadata(w http.ResponseWriter, req *http.Request, avatarID string) {
	if r.avatarReader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar reader is not configured",
		})
		return
	}

	result, err := r.avatarReader.GetAvatarMetadata(req.Context(), avatarID)
	if err != nil {
		if isInternalAvatarReadError(err) {
			r.logInternalError(req.Context(), err, "avatar metadata read failed")
		}
		writeAvatarReadError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, avatarMetadataResponse(result.Avatar))
}

// handleUsers обрабатывает user-scoped API routes
func (r *Router) handleUsers(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodDelete {
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodDelete}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	suffix := strings.TrimPrefix(req.URL.Path, "/api/v1/users/")
	if strings.HasSuffix(suffix, "/avatars") {
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
			return
		}
		userID := strings.TrimSuffix(suffix, "/avatars")
		if userID == "" || strings.Contains(userID, "/") {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "Avatar not found",
			})
			return
		}
		r.handleUserAvatarList(w, req, userID)
		return
	}

	userID, ok := strings.CutSuffix(suffix, "/avatar")
	if !ok || userID == "" || strings.Contains(userID, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Avatar not found",
		})
		return
	}

	if req.Method == http.MethodDelete {
		r.handleLatestAvatarDeleteByUser(w, req, userID)
		return
	}

	result, err := r.avatarReaderResult(req, func(ctx context.Context, size string, format string) (app.AvatarReadResult, error) {
		return r.avatarReader.GetLatestAvatarByUserID(ctx, userID, size, format)
	})
	if err != nil {
		if errors.Is(err, app.ErrAvatarNotFound) {
			if err := ensureDefaultAvatarRequest(req); err != nil {
				writeAvatarReadError(w, err)
				return
			}
			writeDefaultAvatar(w, r.defaultAvatar)
			return
		}
		if isInternalAvatarReadError(err) {
			r.logInternalError(req.Context(), err, "latest avatar read failed")
		}
		writeAvatarReadError(w, err)
		return
	}
	defer func() {
		_ = result.Body.Close()
	}()

	writeAvatarBinary(w, result)
}

// handleLatestAvatarDeleteByUser удаляет последнюю активную avatar пользователя
func (r *Router) handleLatestAvatarDeleteByUser(w http.ResponseWriter, req *http.Request, userID string) {
	if r.avatarDeleter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar delete is not configured",
		})
		return
	}

	requesterUserID, err := validateRequesterUserID(req.Header.Get("X-User-ID"))
	if err != nil {
		writeValidationError(w, err)
		return
	}

	if err := r.avatarDeleter.DeleteLatestAvatarByUserID(req.Context(), userID, requesterUserID); err != nil {
		if isInternalAvatarDeleteError(err) {
			r.logInternalError(req.Context(), err, "latest avatar delete failed")
		}
		writeAvatarDeleteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUserAvatarList возвращает список активных avatar пользователя
func (r *Router) handleUserAvatarList(w http.ResponseWriter, req *http.Request, userID string) {
	if r.avatarReader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "avatar reader is not configured",
		})
		return
	}

	limit, offset := paginationParams(req)
	result, err := r.avatarReader.ListAvatarsByUserID(req.Context(), userID, limit, offset)
	if err != nil {
		if isInternalAvatarReadError(err) {
			r.logInternalError(req.Context(), err, "avatar list failed")
		}
		writeAvatarReadError(w, err)
		return
	}

	items := make([]AvatarMetadataResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, avatarMetadataResponse(item))
	}

	writeJSON(w, http.StatusOK, AvatarListResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
	})
}

const maxUserResolveBodyBytes int64 = 4 * 1024

// handleUserResolve сопоставляет email с внутренним user_id
func (r *Router) handleUserResolve(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}
	if r.userResolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "user resolver is not configured",
		})
		return
	}

	var payload UserResolveRequest
	req.Body = http.MaxBytesReader(w, req.Body, maxUserResolveBodyBytes)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid request",
			"details": "Request body must contain email",
		})
		return
	}

	email, err := validateLookupEmail(payload.Email)
	if err != nil {
		writeValidationError(w, err)
		return
	}

	result, err := r.userResolver.ResolveUserByEmail(req.Context(), email)
	if err != nil {
		r.logInternalError(req.Context(), err, "user resolve failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "User resolve failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, UserResolveResponse{
		ID:        result.ID,
		UserID:    result.ID,
		Email:     result.Email,
		CreatedAt: result.CreatedAt,
		UpdatedAt: result.UpdatedAt,
	})
}

// logInternalError записывает внутреннюю ошибку без потенциально секретного текста
func (r *Router) logInternalError(ctx context.Context, err error, message string) {
	app.LoggerFromContext(ctx).Error().
		Str("error_type", app.ErrorType(err)).
		Msg(message)
}

// isInternalAvatarReadError отличает внутреннюю ошибку чтения от ожидаемой клиентской ошибки
func isInternalAvatarReadError(err error) bool {
	return !errors.Is(err, app.ErrAvatarNotFound) &&
		!errors.Is(err, app.ErrAvatarProcessing) &&
		!errors.Is(err, app.ErrUnsupportedAvatarSize) &&
		!errors.Is(err, app.ErrUnsupportedAvatarFormat)
}

// isInternalAvatarDeleteError отличает внутреннюю ошибку удаления от ожидаемой клиентской ошибки
func isInternalAvatarDeleteError(err error) bool {
	return !errors.Is(err, app.ErrAvatarNotFound) && !errors.Is(err, app.ErrAvatarForbidden)
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
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type UserResolveRequest struct {
	Email string `json:"email"`
}

type UserResolveResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AvatarMetadataResponse struct {
	ID         string            `json:"id"`
	UserID     string            `json:"user_id"`
	FileName   string            `json:"file_name"`
	MimeType   string            `json:"mime_type"`
	SizeBytes  int64             `json:"size_bytes"`
	Width      *int              `json:"width"`
	Height     *int              `json:"height"`
	Status     string            `json:"status"`
	URL        string            `json:"url"`
	Thumbnails []AvatarThumbnail `json:"thumbnails"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type AvatarThumbnail struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

type AvatarListResponse struct {
	Items  []AvatarMetadataResponse `json:"items"`
	Limit  int                      `json:"limit"`
	Offset int                      `json:"offset"`
}

// avatarMetadataResponse собирает JSON metadata avatar
func avatarMetadataResponse(item avatar.Avatar) AvatarMetadataResponse {
	response := AvatarMetadataResponse{
		ID:         item.ID,
		UserID:     item.UserID,
		FileName:   item.FileName,
		MimeType:   item.MimeType,
		SizeBytes:  item.SizeBytes,
		Width:      item.Width,
		Height:     item.Height,
		Status:     string(item.Status),
		URL:        "/api/v1/avatars/" + item.ID,
		Thumbnails: make([]AvatarThumbnail, 0, 2),
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
	if item.Thumb100ObjectKey != nil && *item.Thumb100ObjectKey != "" {
		response.Thumbnails = append(response.Thumbnails, AvatarThumbnail{
			Size: "100x100",
			URL:  "/api/v1/avatars/" + item.ID + "?size=100x100",
		})
	}
	if item.Thumb300ObjectKey != nil && *item.Thumb300ObjectKey != "" {
		response.Thumbnails = append(response.Thumbnails, AvatarThumbnail{
			Size: "300x300",
			URL:  "/api/v1/avatars/" + item.ID + "?size=300x300",
		})
	}
	return response
}

// avatarReaderResult вызывает reader с query параметрами запроса
func (r *Router) avatarReaderResult(req *http.Request, read func(context.Context, string, string) (app.AvatarReadResult, error)) (app.AvatarReadResult, error) {
	if r.avatarReader == nil {
		return app.AvatarReadResult{}, fmt.Errorf("avatar reader is not configured")
	}
	query := req.URL.Query()
	return read(req.Context(), query.Get("size"), query.Get("format"))
}

// writeAvatarReadError записывает HTTP-ответ для ошибки чтения avatar
func writeAvatarReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrAvatarNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Avatar not found"})
	case errors.Is(err, app.ErrAvatarProcessing):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Avatar is still processing"})
	case errors.Is(err, app.ErrUnsupportedAvatarSize):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unsupported avatar size"})
	case errors.Is(err, app.ErrUnsupportedAvatarFormat):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unsupported avatar format"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Avatar read failed"})
	}
}

// writeAvatarUploadError записывает HTTP-ответ для ошибки upload avatar
func writeAvatarUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "User not found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Avatar upload failed"})
	}
}

// writeAvatarDeleteError записывает HTTP-ответ для ошибки удаления avatar
func writeAvatarDeleteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrAvatarNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Avatar not found"})
	case errors.Is(err, app.ErrAvatarForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden", "details": "You can only delete your own avatars"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Avatar delete failed"})
	}
}

// writeAvatarBinary записывает binary avatar response
func writeAvatarBinary(w http.ResponseWriter, result app.AvatarReadResult) {
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "max-age=86400")
	if result.ETag != "" {
		w.Header().Set("ETag", result.ETag)
	}
	if result.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(result.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, result.Body)
}

// writeDefaultAvatar записывает стандартную PNG-заглушку avatar
func writeDefaultAvatar(w http.ResponseWriter, item DefaultAvatar) {
	if len(item.Body) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Default avatar unavailable"})
		return
	}

	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Cache-Control", item.CacheControl)
	w.Header().Set("ETag", item.ETag)
	w.Header().Set("Content-Length", strconv.Itoa(len(item.Body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(item.Body)
}

// ensureDefaultAvatarRequest проверяет query параметры для PNG-заглушки avatar
func ensureDefaultAvatarRequest(req *http.Request) error {
	size := strings.TrimSpace(strings.ToLower(req.URL.Query().Get("size")))
	switch size {
	case "", "original", "100x100", "300x300":
	default:
		return app.ErrUnsupportedAvatarSize
	}

	format := strings.TrimSpace(strings.ToLower(req.URL.Query().Get("format")))
	switch format {
	case "", "png":
		return nil
	default:
		return app.ErrUnsupportedAvatarFormat
	}
}

// paginationParams читает limit и offset с безопасными дефолтами
func paginationParams(req *http.Request) (int, int) {
	query := req.URL.Query()
	limit := 50
	offset := 0

	if rawLimit := query.Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if rawOffset := query.Get("offset"); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
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

// validateRequesterUserID проверяет что X-User-ID содержит внутренний UUID пользователя
func validateRequesterUserID(rawUserID string) (string, error) {
	userID := strings.TrimSpace(rawUserID)
	if userID == "" {
		return "", validationError("Missing X-User-ID", "Header X-User-ID with user id is required")
	}
	parsed, err := uuid.Parse(userID)
	if err != nil {
		return "", validationError("Invalid X-User-ID", "Header X-User-ID must contain user UUID")
	}
	return parsed.String(), nil
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
