package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type corsPolicy struct {
	allowedOrigins map[string]struct{}
}

// newCORSPolicy создает CORS policy с явным allowlist origins
func newCORSPolicy(origins []string) corsPolicy {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" {
			continue
		}
		allowed[origin] = struct{}{}
	}
	return corsPolicy{allowedOrigins: allowed}
}

// allows проверяет разрешен ли Origin для cross-origin запроса
func (p corsPolicy) allows(origin string) bool {
	_, ok := p.allowedOrigins[origin]
	return ok
}

// handleCORS применяет CORS headers и завершает preflight запросы
func (r *Router) handleCORS(w http.ResponseWriter, req *http.Request) bool {
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	if !r.cors.allows(origin) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "CORS origin forbidden",
		})
		return true
	}

	addVaryHeader(w.Header(), "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID, X-Request-ID")
	w.Header().Set("Access-Control-Expose-Headers", "ETag, X-Request-ID")
	w.Header().Set("Access-Control-Max-Age", "600")
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// addVaryHeader добавляет значение Vary без дублей
func addVaryHeader(header http.Header, value string) {
	existing := header.Values("Vary")
	for _, line := range existing {
		for _, item := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

type clientRateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	clients map[string]*rateLimitBucket
	now     func() time.Time
}

type rateLimitBucket struct {
	tokens    float64
	updatedAt time.Time
}

// newClientRateLimiter создает token bucket limiter на клиента
func newClientRateLimiter(rps int, burst int) *clientRateLimiter {
	if rps <= 0 || burst <= 0 {
		return nil
	}
	return &clientRateLimiter{
		rate:    float64(rps),
		burst:   float64(burst),
		clients: make(map[string]*rateLimitBucket),
		now:     time.Now,
	}
}

// shouldLimit проверяет нужно ли применять limiter к запросу
func (r *Router) shouldLimit(req *http.Request) bool {
	return r.rateLimiter != nil && strings.HasPrefix(req.URL.Path, "/api/")
}

// allowRequest проверяет запрос через per-client limiter
func (r *Router) allowRequest(req *http.Request) bool {
	if r.rateLimiter == nil {
		return true
	}
	return r.rateLimiter.allow(clientIP(req))
}

// allow списывает token из bucket клиента
func (l *clientRateLimiter) allow(client string) bool {
	if client == "" {
		client = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	bucket, ok := l.clients[client]
	if !ok {
		l.clients[client] = &rateLimitBucket{
			tokens:    l.burst - 1,
			updatedAt: now,
		}
		return true
	}

	elapsed := now.Sub(bucket.updatedAt).Seconds()
	bucket.tokens = minFloat(l.burst, bucket.tokens+elapsed*l.rate)
	bucket.updatedAt = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// clientIP определяет ключ клиента для rate limiting
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	return req.RemoteAddr
}

// minFloat возвращает меньшее из двух float64 значений
func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
