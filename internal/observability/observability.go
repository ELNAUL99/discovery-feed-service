package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Logger *zap.Logger
	Tracer oteltrace.Tracer

	// Prometheus metrics
	FeedRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "feed_requests_total",
		Help: "Total number of feed requests",
	}, []string{"variant", "status"})

	FeedRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "feed_request_duration_seconds",
		Help:    "Duration of feed requests in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"variant"})

	RankingScoreComputed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ranking_scores_computed_total",
		Help: "Total number of ranking scores computed",
	}, []string{"experiment"})

	CacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_hits_total",
		Help: "Total number of cache hits",
	}, []string{"cache_type"})

	CacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_misses_total",
		Help: "Total number of cache misses",
	}, []string{"cache_type"})

	DBQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Duration of database queries in seconds",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"operation"})

	AIRecommendationsGenerated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ai_recommendations_generated_total",
		Help: "Total number of AI recommendations generated",
	})

	ExperimentConversions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "experiment_conversions_total",
		Help: "Total number of experiment conversions",
	}, []string{"experiment_id", "variant_id"})
)

func InitLogger(env string) (*zap.Logger, error) {
	var config zap.Config
	if env == "production" {
		config = zap.NewProductionConfig()
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	Logger = logger
	return logger, nil
}

func InitTracer(serviceName, env string, enabled bool) (*sdktrace.TracerProvider, error) {
	if !enabled {
		Tracer = otel.Tracer("noop")
		return nil, nil
	}

	exp, err := jaeger.New(jaeger.WithAgentEndpoint(
		jaeger.WithAgentHost("jaeger"),
		jaeger.WithAgentPort("6831"),
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create jaeger exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithExportTimeout(30*time.Second),
		),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			attribute.String("service.name", serviceName),
			attribute.String("service.environment", env),
		)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	Tracer = tp.Tracer(serviceName)
	return tp, nil
}

func StartSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	if Tracer == nil {
		return otel.Tracer("noop").Start(ctx, name, opts...)
	}
	return Tracer.Start(ctx, name, opts...)
}

func LogInfo(msg string, fields ...zap.Field) {
	if Logger != nil {
		Logger.Info(msg, fields...)
	}
}

func LogError(msg string, err error, fields ...zap.Field) {
	if Logger != nil {
		fields = append(fields, zap.Error(err))
		Logger.Error(msg, fields...)
	}
}

func LogWarn(msg string, fields ...zap.Field) {
	if Logger != nil {
		Logger.Warn(msg, fields...)
	}
}

func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		// Start trace span
		ctx, span := Tracer.Start(c.Request.Context(), fmt.Sprintf("HTTP %s %s", c.Request.Method, path))
		defer span.End()

		c.Request = c.Request.WithContext(ctx)

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()
		variant := c.GetString("experiment_variant")
		if variant == "" {
			variant = "control"
		}

		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.path", path),
			attribute.Int("http.status_code", status),
			attribute.String("experiment.variant", variant),
			attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
		)

		FeedRequestsTotal.WithLabelValues(variant, fmt.Sprintf("%d", status)).Inc()
		FeedRequestDuration.WithLabelValues(variant).Observe(duration.Seconds())

		LogInfo("HTTP request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("duration", duration),
			zap.String("variant", variant),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}
