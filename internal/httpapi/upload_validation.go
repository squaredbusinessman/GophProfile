package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

const (
	// MaxAvatarFileSize задаёт максимальный размер загружаемого аватара в байтах
	MaxAvatarFileSize       int64 = 10 * 1024 * 1024
	avatarUploadMemoryLimit int64 = 1 * 1024 * 1024
	avatarUploadBodyLimit   int64 = MaxAvatarFileSize + avatarUploadMemoryLimit
)

// ValidatedAvatarUpload содержит проверенные данные загружаемого файла
type ValidatedAvatarUpload struct {
	// UserID содержит идентификатор владельца
	UserID string
	// FileName содержит исходное имя файла
	FileName string
	// ContentType содержит проверенный MIME-тип
	ContentType string
	// Size содержит размер файла в байтах
	Size int64
	// FileHeader содержит заголовок файла multipart
	FileHeader *multipart.FileHeader
	form       *multipart.Form
}

// ValidationError описывает безопасную ошибку клиентского запроса
type ValidationError struct {
	// StatusCode содержит HTTP-код ответа
	StatusCode int `json:"-"`
	// Message содержит краткое описание ошибки
	Message string `json:"error"`
	// Details содержит безопасные дополнительные сведения
	Details string `json:"details,omitempty"`
}

// Error возвращает человекочитаемое описание ошибки валидации
func (e *ValidationError) Error() string {
	if e.Details == "" {
		return e.Message
	}
	return e.Message + ": " + e.Details
}

// Open открывает проверенный файл multipart для дальнейшего чтения
func (u *ValidatedAvatarUpload) Open() (multipart.File, error) {
	return u.FileHeader.Open()
}

// Close удаляет временные файлы формы multipart
func (u *ValidatedAvatarUpload) Close() error {
	if u.form == nil {
		return nil
	}
	return u.form.RemoveAll()
}

// ValidateAvatarUploadRequest проверяет запрос загрузки аватара до обращения к S3
func ValidateAvatarUploadRequest(w http.ResponseWriter, req *http.Request) (*ValidatedAvatarUpload, error) {
	userID, err := validateRequesterUserID(req.Header.Get("X-User-ID"))
	if err != nil {
		return nil, err
	}

	req.Body = http.MaxBytesReader(w, req.Body, avatarUploadBodyLimit)
	if err := req.ParseMultipartForm(avatarUploadMemoryLimit); err != nil {
		return nil, parseMultipartError(err)
	}

	fileHeader, err := requiredFileHeader(req.MultipartForm)
	if err != nil {
		return nil, err
	}
	if fileHeader.Size > MaxAvatarFileSize {
		return nil, fileTooLargeError()
	}

	contentType, err := normalizeContentType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, validationError("Invalid file", "Cannot open uploaded file")
	}

	magicContentType, err := detectImageContentType(file)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, validationError("Invalid file", "Cannot close uploaded file")
	}
	if magicContentType != contentType {
		return nil, validationError("Invalid file format", "MIME type does not match file content")
	}

	return &ValidatedAvatarUpload{
		UserID:      userID,
		FileName:    fileHeader.Filename,
		ContentType: contentType,
		Size:        fileHeader.Size,
		FileHeader:  fileHeader,
		form:        req.MultipartForm,
	}, nil
}

// parseMultipartError преобразует ошибку разбора multipart в ошибку API
func parseMultipartError(err error) error {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return fileTooLargeError()
	}
	return validationError("Invalid multipart form", "Request must include multipart field file")
}

// requiredFileHeader возвращает обязательный заголовок файла multipart
func requiredFileHeader(form *multipart.Form) (*multipart.FileHeader, error) {
	if form == nil || form.File == nil {
		return nil, validationError("Missing file", "Multipart field file is required")
	}

	files := form.File["file"]
	if len(files) == 0 {
		return nil, validationError("Missing file", "Multipart field file is required")
	}

	fileHeader := files[0]
	if fileHeader.Size == 0 {
		return nil, validationError("Invalid file format", "File is empty")
	}
	return fileHeader, nil
}

// normalizeContentType проверяет и нормализует Content-Type multipart file
func normalizeContentType(rawContentType string) (string, error) {
	contentType := strings.TrimSpace(strings.ToLower(rawContentType))
	if contentType == "" {
		return "", validationError("Invalid file format", "Missing file Content-Type")
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", validationError("Invalid file format", "Invalid file Content-Type")
	}
	if !isAllowedImageContentType(mediaType) {
		return "", validationError("Invalid file format", "Supported formats: jpeg, png, webp")
	}
	return mediaType, nil
}

// detectImageContentType определяет MIME-тип по magic bytes
func detectImageContentType(reader io.Reader) (string, error) {
	buffer := make([]byte, 512)
	n, err := io.ReadFull(reader, buffer)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", validationError("Invalid file format", "Cannot read file header")
	}
	if n == 0 {
		return "", validationError("Invalid file format", "File is empty")
	}

	contentType := detectMagicContentType(buffer[:n])
	if contentType == "" {
		return "", validationError("Invalid file format", "Supported formats: jpeg, png, webp")
	}
	return contentType, nil
}

// detectMagicContentType распознает JPEG PNG и WebP по magic bytes
func detectMagicContentType(data []byte) string {
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 8 &&
		data[0] == 0x89 &&
		data[1] == 'P' &&
		data[2] == 'N' &&
		data[3] == 'G' &&
		data[4] == '\r' &&
		data[5] == '\n' &&
		data[6] == 0x1a &&
		data[7] == '\n' {
		return "image/png"
	}
	if len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

// isAllowedImageContentType проверяет разрешенный MIME-тип изображения
func isAllowedImageContentType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

// fileTooLargeError возвращает ошибку превышения размера файла
func fileTooLargeError() error {
	return &ValidationError{
		StatusCode: http.StatusRequestEntityTooLarge,
		Message:    "File too large",
		Details:    fmt.Sprintf("Max size is %d bytes", MaxAvatarFileSize),
	}
}

// validationError создает ошибку валидации для HTTP-ответа
func validationError(message string, details string) error {
	return &ValidationError{
		StatusCode: http.StatusBadRequest,
		Message:    message,
		Details:    details,
	}
}
