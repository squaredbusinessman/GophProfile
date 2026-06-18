package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Telemetry содержит провайдеры и HTTP-обработчик метрик
type Telemetry struct {
	// TracerProvider создаёт и накапливает трассировки приложения
	TracerProvider *sdktrace.TracerProvider
	// MeterProvider создаёт метрики приложения
	MeterProvider *metric.MeterProvider
	// MetricsHandler отдаёт метрики в формате Prometheus
	MetricsHandler http.Handler
	// Resource содержит общие атрибуты сервиса
	Resource *resource.Resource

	// mu защищает состояние сервера метрик
	mu sync.Mutex
	// metricsServer обслуживает отдельный HTTP-адрес метрик
	metricsServer *http.Server
	// metricsDone принимает результат завершения сервера метрик
	metricsDone chan error
}

// NewTelemetry создаёт и регистрирует провайдеры телеметрии
func NewTelemetry(ctx context.Context, cfg config.Config) (*Telemetry, error) {
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.Observability.ServiceName),
		semconv.ServiceVersion(cfg.Version),
		semconv.ServiceInstanceID(uuid.NewString()),
		attribute.String("deployment.environment", cfg.Env),
	)
	registry := prometheus.NewRegistry()
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})

	var tp *sdktrace.TracerProvider
	var mp *metric.MeterProvider
	if cfg.Observability.Enabled {
		exporter, err := newTraceExporter(ctx, cfg.Observability)
		if err != nil {
			return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
		}
		promExporter, err := otelprom.New(otelprom.WithRegisterer(registry))
		if err != nil {
			_ = exporter.Shutdown(ctx)
			return nil, fmt.Errorf("create Prometheus exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(traceSampler(cfg.Observability)),
			sdktrace.WithBatcher(exporter),
		)
		mp = metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(promExporter))
	} else {
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.NeverSample()),
		)
		mp = metric.NewMeterProvider(metric.WithResource(res))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Telemetry{
		TracerProvider: tp,
		MeterProvider:  mp,
		MetricsHandler: handler,
		Resource:       res,
	}, nil
}

// newTraceExporter создаёт OTLP-экспортёр трассировок с настройками подключения
func newTraceExporter(ctx context.Context, cfg config.ObservabilityConfig) (*otlptrace.Exporter, error) {
	opts := make([]otlptracegrpc.Option, 0, 2)
	if parsed, err := url.Parse(cfg.OTLPEndpoint); err == nil && parsed.Scheme != "" {
		opts = append(opts, otlptracegrpc.WithEndpointURL(cfg.OTLPEndpoint))
	} else {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}

// traceSampler создаёт стратегию семплирования трассировок из конфигурации
func traceSampler(cfg config.ObservabilityConfig) sdktrace.Sampler {
	ratio := sdktrace.TraceIDRatioBased(cfg.TracesSamplerArg)
	switch strings.ToLower(strings.TrimSpace(cfg.TracesSampler)) {
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return ratio
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(ratio)
	case "always_on":
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

// StartMetricsServer занимает адрес и запускает раздачу метрик в фоне
func (t *Telemetry) StartMetricsServer(addr string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.metricsServer != nil {
		return errors.New("metrics server already started")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen metrics: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", t.MetricsHandler)
	t.metricsServer = &http.Server{Addr: addr, Handler: mux}
	t.metricsDone = make(chan error, 1)
	go func() {
		err := t.metricsServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		if err != nil {
			otel.Handle(fmt.Errorf("metrics server: %w", err))
		}
		t.metricsDone <- err
	}()
	return nil
}

// Shutdown останавливает сервер метрик, отправляет накопленные интервалы трассировки и завершает провайдеры
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	server := t.metricsServer
	done := t.metricsDone
	t.mu.Unlock()

	var errs []error
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown metrics server: %w", err))
		} else if err := <-done; err != nil {
			errs = append(errs, fmt.Errorf("serve metrics: %w", err))
		}
	}
	if t.TracerProvider != nil {
		if err := t.TracerProvider.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush traces: %w", err))
		}
		if err := t.TracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown tracer provider: %w", err))
		}
	}
	if t.MeterProvider != nil {
		if err := t.MeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown meter provider: %w", err))
		}
	}
	return errors.Join(errs...)
}
