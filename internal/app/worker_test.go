package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestPublishPendingOutboxCreatesWorkerSpan проверяет business span фоновой публикации
func TestPublishPendingOutboxCreatesWorkerSpan(t *testing.T) {
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	service := NewOutboxPublisherService(&fakeOutboxEventStore{events: []outbox.Event{}}, &fakeEventPublisher{})
	publishPendingOutbox(context.Background(), config.Config{Worker: config.WorkerConfig{OutboxBatchSize: 10}}, service)

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "worker.outbox.publish" {
		t.Fatalf("worker spans = %#v, want worker.outbox.publish", spans)
	}
}

// TestRunWorkerWaitsForShutdownTimeout проверяет ожидание graceful timeout после отмены context
func TestRunWorkerWaitsForShutdownTimeout(t *testing.T) {
	cfg := config.Config{
		Kafka: config.KafkaConfig{
			Brokers:       []string{"localhost:9092"},
			ConsumerGroup: "test-group",
		},
		Worker: config.WorkerConfig{
			ShutdownTimeout:    50 * time.Millisecond,
			OutboxPollInterval: time.Hour,
			OutboxBatchSize:    1,
		},
	}
	consumer := &blockingWorkerConsumer{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer close(consumer.release)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		result <- RunWorker(ctx, cfg, zerolog.Nop(), nil, consumer, &AvatarProcessService{}, nil)
	}()

	select {
	case <-consumer.started:
	case <-time.After(time.Second):
		t.Fatal("worker consumer did not start")
	}

	cancelledAt := time.Now()
	cancel()

	select {
	case <-consumer.stopped:
	case <-time.After(time.Second):
		t.Fatal("worker consumer did not receive cancelled context")
	}

	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RunWorker error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(cancelledAt); elapsed < 40*time.Millisecond {
			t.Fatalf("RunWorker returned too early after %s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("RunWorker did not return after shutdown timeout")
	}
}

type blockingWorkerConsumer struct {
	started chan struct{}
	stopped chan struct{}
	release chan struct{}
}

// Consume ожидает отмену context и удерживает consumer до release
func (c *blockingWorkerConsumer) Consume(ctx context.Context, _ []string, _ func(context.Context, queuekafka.Message) error) error {
	close(c.started)
	<-ctx.Done()
	close(c.stopped)
	<-c.release
	return ctx.Err()
}
