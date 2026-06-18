package app

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
)

// RunWorker запускает worker и периодически публикует pending outbox события
func RunWorker(ctx context.Context, cfg config.Config, logger zerolog.Logger, outboxPublisher *OutboxPublisherService, processConsumer ProcessMessageConsumer, avatarProcessor *AvatarProcessService, avatarDeleter *AvatarDeleteWorkerService) error {
	ctx = ContextWithLogger(ctx, logger)
	LoggerFromContext(ctx).Info().
		Strs("kafka_brokers", cfg.Kafka.Brokers).
		Str("consumer_group", cfg.Kafka.ConsumerGroup).
		Msg("worker started")

	var consumerErrCh <-chan error
	if processConsumer != nil && (avatarProcessor != nil || avatarDeleter != nil) {
		errCh := make(chan error, 1)
		consumerErrCh = errCh
		go func() {
			errCh <- consumeAvatarMessages(ctx, processConsumer, avatarProcessor, avatarDeleter)
		}()
	}

	ticker := time.NewTicker(cfg.Worker.OutboxPollInterval)
	defer ticker.Stop()

	publishPendingOutbox(ctx, cfg, outboxPublisher)

	for {
		select {
		case err := <-consumerErrCh:
			return err
		case <-ctx.Done():
			return shutdownWorker(ctx, cfg, consumerErrCh)
		case <-ticker.C:
			publishPendingOutbox(ctx, cfg, outboxPublisher)
		}
	}
}

type ProcessMessageConsumer interface {
	Consume(ctx context.Context, topics []string, handler func(context.Context, queuekafka.Message) error) error
}

// consumeAvatarMessages читает avatar topics и запускает нужный обработчик
func consumeAvatarMessages(ctx context.Context, consumer ProcessMessageConsumer, processor *AvatarProcessService, deleter *AvatarDeleteWorkerService) error {
	topics := make([]string, 0, 5)
	if processor != nil {
		topics = append(topics,
			queuekafka.TopicAvatarProcess,
			queuekafka.TopicAvatarProcessRetry1m,
			queuekafka.TopicAvatarProcessRetry5m,
			queuekafka.TopicAvatarProcessRetry30m,
		)
	}
	if deleter != nil {
		topics = append(topics, queuekafka.TopicAvatarDelete)
	}
	return consumer.Consume(ctx, topics, func(ctx context.Context, message queuekafka.Message) error {
		if message.Topic == queuekafka.TopicAvatarDelete {
			return deleter.HandleDeleteMessage(ctx, message.Value)
		}
		return processor.HandleProcessMessage(ctx, message.Value)
	})
}

// shutdownWorker выполняет graceful shutdown worker
func shutdownWorker(ctx context.Context, cfg config.Config, consumerErrCh <-chan error) error {
	LoggerFromContext(ctx).Info().Dur("timeout", cfg.Worker.ShutdownTimeout).Msg("worker shutting down")

	if consumerErrCh == nil {
		return ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Worker.ShutdownTimeout)
	defer cancel()

	select {
	case err := <-consumerErrCh:
		return err
	case <-shutdownCtx.Done():
		return fmt.Errorf("worker shutdown timeout: %w", shutdownCtx.Err())
	}
}

// publishPendingOutbox публикует pending outbox события если publisher настроен
func publishPendingOutbox(ctx context.Context, cfg config.Config, outboxPublisher *OutboxPublisherService) {
	if outboxPublisher == nil {
		return
	}

	published, err := outboxPublisher.PublishPending(ctx, cfg.Worker.OutboxBatchSize)
	if err != nil {
		LoggerFromContext(ctx).Error().
			Str("error_type", ErrorType(err)).
			Msg("outbox publish failed")
		return
	}
	if published > 0 {
		LoggerFromContext(ctx).Info().Int("published", published).Msg("outbox events published")
	}
}
