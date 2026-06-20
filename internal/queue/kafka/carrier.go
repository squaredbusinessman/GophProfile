// Package kafka предоставляет инструментированный клиент Kafka и перенос контекста трассировки
package kafka

import (
	"context"
	"sort"
	"strings"

	confluent "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"go.opentelemetry.io/otel"
)

// HeaderCarrier адаптирует заголовки Confluent Kafka к интерфейсу W3C TextMapCarrier
type HeaderCarrier []confluent.Header

// Get возвращает последнее значение заголовка без учёта регистра имени
func (c HeaderCarrier) Get(key string) string {
	for index := len(c) - 1; index >= 0; index-- {
		if strings.EqualFold(c[index].Key, key) {
			return string(c[index].Value)
		}
	}
	return ""
}

// Set заменяет заголовок трассировки и сохраняет остальные заголовки
func (c *HeaderCarrier) Set(key string, value string) {
	filtered := make(HeaderCarrier, 0, len(*c)+1)
	for _, header := range *c {
		if !strings.EqualFold(header.Key, key) {
			filtered = append(filtered, header)
		}
	}
	*c = append(filtered, confluent.Header{Key: key, Value: []byte(value)})
}

// Keys возвращает уникальные имена заголовков Kafka
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	seen := make(map[string]struct{}, len(c))
	for _, header := range c {
		normalized := strings.ToLower(header.Key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keys = append(keys, header.Key)
	}
	return keys
}

// Map преобразует заголовки Confluent в независимое отображение для outbox
func (c HeaderCarrier) Map() map[string]string {
	headers := make(map[string]string, len(c))
	for _, header := range c {
		headers[header.Key] = string(header.Value)
	}
	return headers
}

// headerCarrierFromMap создаёт детерминированный носитель заголовков Confluent
func headerCarrierFromMap(headers map[string]string) HeaderCarrier {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	carrier := make(HeaderCarrier, 0, len(keys))
	for _, key := range keys {
		carrier = append(carrier, confluent.Header{Key: key, Value: []byte(headers[key])})
	}
	return carrier
}

// InjectTraceContext добавляет контекст трассировки W3C и сохраняет исходные заголовки
func InjectTraceContext(ctx context.Context, headers map[string]string) map[string]string {
	carrier := headerCarrierFromMap(headers)
	otel.GetTextMapPropagator().Inject(ctx, &carrier)
	return carrier.Map()
}

// ExtractTraceContext восстанавливает удалённый контекст трассировки из заголовков Kafka
func ExtractTraceContext(ctx context.Context, headers map[string]string) context.Context {
	carrier := headerCarrierFromMap(headers)
	return otel.GetTextMapPropagator().Extract(ctx, &carrier)
}

type messageHeadersContextKey struct{}

// contextWithMessageHeaders сохраняет входящие заголовки для повторной обработки и недоставленных сообщений
func contextWithMessageHeaders(ctx context.Context, headers map[string]string) context.Context {
	return context.WithValue(ctx, messageHeadersContextKey{}, cloneHeaders(headers))
}

// messageHeadersFromContext возвращает независимую копию входящих заголовков
func messageHeadersFromContext(ctx context.Context) map[string]string {
	headers, _ := ctx.Value(messageHeadersContextKey{}).(map[string]string)
	return cloneHeaders(headers)
}

// cloneHeaders копирует заголовки без изменения исходного отображения
func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}
