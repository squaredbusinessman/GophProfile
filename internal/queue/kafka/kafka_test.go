package kafka

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	confluent "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestHeaderCarrierInjectExtractRoundTrip проверяет перенос контекста W3C и неизвестных заголовков
func TestHeaderCarrierInjectExtractRoundTrip(t *testing.T) {
	_, _, _ = installKafkaTestProviders(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "http request")
	traceState, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatalf("ParseTraceState() error = %v", err)
	}
	want := span.SpanContext().WithTraceState(traceState)
	ctx = trace.ContextWithSpanContext(ctx, want)
	headers := InjectTraceContext(ctx, map[string]string{"x-custom": "preserved"})
	span.End()

	if headers["traceparent"] == "" || headers["tracestate"] != "vendor=value" || headers["x-custom"] != "preserved" {
		t.Fatalf("injected headers = %v", headers)
	}
	extracted := trace.SpanContextFromContext(ExtractTraceContext(context.Background(), headers))
	if !extracted.IsRemote() || extracted.TraceID() != want.TraceID() || extracted.SpanID() != want.SpanID() || extracted.TraceState().String() != "vendor=value" {
		t.Fatalf("extracted span context = %v, want remote %v", extracted, want)
	}
}

// TestImmediateAndDelayedPublishKeepTraceID проверяет сохранённый носитель заголовков outbox
func TestImmediateAndDelayedPublishKeepTraceID(t *testing.T) {
	recorder, _, _ := installKafkaTestProviders(t)
	producer := &fakeProducer{}
	client := newTestClient(producer, &fakeConsumer{}, "workers")
	ctx, root := otel.Tracer("test").Start(context.Background(), "HTTP POST /avatars")
	traceID := root.SpanContext().TraceID()
	headers := InjectTraceContext(ctx, map[string]string{"x-custom": "preserved"})
	root.End()

	if err := client.Publish(ctx, TopicAvatarProcess, "secret-key", []byte("secret-payload"), headers); err != nil {
		t.Fatalf("immediate Publish() error = %v", err)
	}
	if err := client.Publish(context.Background(), TopicAvatarProcess, "secret-key", []byte("secret-payload"), headers); err != nil {
		t.Fatalf("delayed Publish() error = %v", err)
	}

	sendSpans := 0
	for _, span := range recorder.Ended() {
		if span.Name() == "send "+TopicAvatarProcess {
			sendSpans++
			if span.SpanContext().TraceID() != traceID {
				t.Fatalf("send trace ID = %s, want %s", span.SpanContext().TraceID(), traceID)
			}
		}
	}
	if sendSpans != 2 {
		t.Fatalf("send spans = %d, want 2", sendSpans)
	}
	if producer.messages[1].Headers == nil || HeaderCarrier(producer.messages[1].Headers).Get("x-custom") != "preserved" {
		t.Fatalf("delayed headers = %#v", producer.messages[1].Headers)
	}
}

// TestConsumerHandlerReceivesRemoteParentAndCommits проверяет удалённого родителя и фиксацию смещения
func TestConsumerHandlerReceivesRemoteParentAndCommits(t *testing.T) {
	recorder, _, _ := installKafkaTestProviders(t)
	ctx, root := otel.Tracer("test").Start(context.Background(), "send source")
	rootContext := root.SpanContext()
	headers := headerCarrierFromMap(InjectTraceContext(ctx, nil))
	root.End()
	topic := TopicAvatarProcess
	consumer := &fakeConsumer{events: []confluent.Event{&confluent.Message{
		TopicPartition: confluent.TopicPartition{Topic: &topic, Partition: 2, Offset: 42},
		Headers:        headers,
		Key:            []byte("secret-key"),
		Value:          []byte("secret-payload"),
	}}}
	client := newTestClient(&fakeProducer{}, consumer, "workers")
	consumeCtx, cancel := context.WithCancel(context.Background())

	var handlerContext trace.SpanContext
	var received Message
	err := client.Consume(consumeCtx, []string{topic}, func(ctx context.Context, message Message) error {
		handlerContext = trace.SpanContextFromContext(ctx)
		received = message
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() error = %v, want context canceled", err)
	}
	if handlerContext.TraceID() != rootContext.TraceID() || received.Partition != 2 || received.Offset != 42 {
		t.Fatalf("handler context = %v message = %#v", handlerContext, received)
	}
	if consumer.commits != 1 {
		t.Fatalf("commits = %d, want 1", consumer.commits)
	}

	for _, span := range recorder.Ended() {
		if span.Name() == "process "+topic && span.Parent().SpanID() != rootContext.SpanID() {
			t.Fatalf("consumer parent = %s, want %s", span.Parent().SpanID(), rootContext.SpanID())
		}
	}
}

// TestHandlerErrorDoesNotCommitOffset проверяет отсутствие фиксации смещения после ошибки обработчика
func TestHandlerErrorDoesNotCommitOffset(t *testing.T) {
	_, metricsHandler, _ := installKafkaTestProviders(t)
	topic := TopicAvatarDelete
	consumer := &fakeConsumer{events: []confluent.Event{&confluent.Message{
		TopicPartition: confluent.TopicPartition{Topic: &topic, Partition: 1, Offset: 9},
		Key:            []byte("secret-key"),
		Value:          []byte("secret-payload"),
	}}}
	client := newTestClient(&fakeProducer{}, consumer, "workers")
	consumeCtx, cancel := context.WithCancel(context.Background())
	err := client.Consume(consumeCtx, []string{topic}, func(context.Context, Message) error {
		cancel()
		return errors.New("handler failed")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() error = %v, want context canceled", err)
	}
	if consumer.commits != 0 {
		t.Fatalf("commits = %d, want 0", consumer.commits)
	}

	metrics := scrapeKafkaMetrics(t, metricsHandler)
	if !strings.Contains(metrics, `messaging_operation_name="process"`) ||
		!strings.Contains(metrics, `messaging_operation_result="error"`) {
		t.Fatalf("process error metrics are missing: %s", metrics)
	}
	if strings.Contains(metrics, "secret-key") || strings.Contains(metrics, "secret-payload") {
		t.Fatalf("metrics contain message data: %s", metrics)
	}
}

// TestRetryPublishPreservesTraceAndUnknownHeaders проверяет контекст повторного сообщения и неизвестные заголовки
func TestRetryPublishPreservesTraceAndUnknownHeaders(t *testing.T) {
	_, _, _ = installKafkaTestProviders(t)
	ctx, root := otel.Tracer("test").Start(context.Background(), "send source")
	traceID := root.SpanContext().TraceID()
	headers := InjectTraceContext(ctx, map[string]string{"x-custom": "preserved"})
	root.End()
	topic := TopicAvatarProcess
	consumer := &fakeConsumer{events: []confluent.Event{&confluent.Message{
		TopicPartition: confluent.TopicPartition{Topic: &topic, Partition: 0, Offset: 1},
		Headers:        headerCarrierFromMap(headers),
	}}}
	producer := &fakeProducer{}
	client := newTestClient(producer, consumer, "workers")
	consumeCtx, cancel := context.WithCancel(context.Background())

	err := client.Consume(consumeCtx, []string{topic}, func(ctx context.Context, _ Message) error {
		if err := client.Publish(ctx, TopicAvatarProcessRetry1m, "secret-key", []byte("secret-payload"), nil); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() error = %v, want context canceled", err)
	}
	producedHeaders := HeaderCarrier(producer.messages[0].Headers)
	extracted := trace.SpanContextFromContext(ExtractTraceContext(context.Background(), producedHeaders.Map()))
	if extracted.TraceID() != traceID || producedHeaders.Get("x-custom") != "preserved" {
		t.Fatalf("retry trace = %s headers = %#v", extracted.TraceID(), producedHeaders)
	}
}

// newTestClient создаёт тестовый клиент Kafka
func newTestClient(producer producerAPI, consumer consumerAPI, group string) *Client {
	return &Client{producer: producer, consumer: consumer, consumerGroup: group, telemetry: newKafkaTelemetry()}
}

// fakeProducer синхронно возвращает отчёт об успешной доставке
type fakeProducer struct {
	messages   []*confluent.Message
	produceErr error
}

// Produce сохраняет сообщение и отправляет отчёт о доставке
func (f *fakeProducer) Produce(message *confluent.Message, deliveryChan chan confluent.Event) error {
	if f.produceErr != nil {
		return f.produceErr
	}
	copyMessage := *message
	copyMessage.Headers = append([]confluent.Header(nil), message.Headers...)
	copyMessage.TopicPartition.Partition = 2
	f.messages = append(f.messages, &copyMessage)
	deliveryChan <- &copyMessage
	return nil
}

// GetMetadata возвращает пустые тестовые метаданные
func (*fakeProducer) GetMetadata(*string, bool, int) (*confluent.Metadata, error) {
	return &confluent.Metadata{}, nil
}

// Flush завершает тестового производителя без ожидающих сообщений
func (*fakeProducer) Flush(int) int { return 0 }

// Close завершает тестового производителя
func (*fakeProducer) Close() {}

// fakeConsumer выдаёт заранее заданные события и считает фиксации смещений
type fakeConsumer struct {
	events    []confluent.Event
	index     int
	commits   int
	commitErr error
}

// SubscribeTopics принимает тестовую подписку
func (*fakeConsumer) SubscribeTopics([]string, confluent.RebalanceCb) error { return nil }

// Poll возвращает следующее тестовое событие
func (f *fakeConsumer) Poll(int) confluent.Event {
	if f.index >= len(f.events) {
		return nil
	}
	event := f.events[f.index]
	f.index++
	return event
}

// CommitMessage считает успешные попытки фиксации смещения
func (f *fakeConsumer) CommitMessage(*confluent.Message) ([]confluent.TopicPartition, error) {
	if f.commitErr != nil {
		return nil, f.commitErr
	}
	f.commits++
	return nil, nil
}

// Close завершает тестового потребителя
func (*fakeConsumer) Close() error { return nil }

// installKafkaTestProviders устанавливает распространитель W3C и тестовые провайдеры
func installKafkaTestProviders(t *testing.T) (*tracetest.SpanRecorder, http.Handler, *sdktrace.TracerProvider) {
	t.Helper()
	previousTracer := otel.GetTracerProvider()
	previousMeter := otel.GetMeterProvider()
	previousPropagator := otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	registry := prometheus.NewRegistry()
	prometheusExporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("create Prometheus exporter: %v", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(prometheusExporter))
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
		otel.SetTracerProvider(previousTracer)
		otel.SetMeterProvider(previousMeter)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return recorder, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), tracerProvider
}

// scrapeKafkaMetrics возвращает метрики в формате Prometheus
func scrapeKafkaMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return recorder.Body.String()
}
