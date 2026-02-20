package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/discovery-feed-service/internal/ai"
	"github.com/discovery-feed-service/internal/config"
	"github.com/discovery-feed-service/internal/experiment"
	"github.com/discovery-feed-service/internal/handler"
	"github.com/discovery-feed-service/internal/middleware"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/ranking"
	"github.com/discovery-feed-service/internal/repository/postgres"
	"github.com/discovery-feed-service/internal/repository/redis"
	"github.com/discovery-feed-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize logger
	logger, err := observability.InitLogger(cfg.Server.Environment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	observability.LogInfo("starting discovery feed service",
		zap.String("environment", cfg.Server.Environment),
		zap.String("port", cfg.Server.Port),
	)

	// Initialize tracer
	tracerProvider, err := observability.InitTracer("discovery-feed-service", cfg.Server.Environment, cfg.Features.EnableTracing)
	if err != nil {
		observability.LogError("failed to initialize tracer", err)
	}
	if tracerProvider != nil {
		defer tracerProvider.Shutdown(context.Background())
	}

	// Initialize Postgres
	pgRepo, err := postgres.NewRepository(cfg.Database.DSN())
	if err != nil {
		observability.LogError("failed to connect to postgres", err)
		os.Exit(1)
	}
	defer pgRepo.Close()
	observability.LogInfo("connected to postgres")

	// Initialize Redis
	redisRepo := redis.NewRepository(cfg.Redis.Addr(), cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.PoolSize)
	if err := redisRepo.Ping(context.Background()); err != nil {
		observability.LogError("failed to connect to redis", err)
		// Redis is intentionally non-fatal because Postgres can still serve the
		// canonical feed; losing Redis mainly removes freshness, caching, and
		// personalization signals instead of taking discovery fully offline.
		observability.LogInfo("continuing without redis cache")
	} else {
		observability.LogInfo("connected to redis")
	}
	defer redisRepo.Close()

	// Initialize services
	rankingEngine := ranking.NewEngine(pgRepo, redisRepo)
	experimentSvc := experiment.NewService(pgRepo, redisRepo)
	personalizationSvc := service.NewPersonalizationService(pgRepo, redisRepo)
	feedSvc := service.NewFeedService(pgRepo, redisRepo, rankingEngine, experimentSvc, personalizationSvc)

	// Initialize AI agent
	aiAgent := ai.NewAgent(rankingEngine, cfg.AI)

	// Initialize handlers
	feedHandler := handler.NewFeedHandler(feedSvc)
	experimentHandler := handler.NewExperimentHandler(experimentSvc)
	aiHandler := handler.NewAIHandler(aiAgent)
	adminHandler := handler.NewAdminHandler(rankingEngine)

	// Setup Gin
	if cfg.Server.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(observability.GinMiddleware())
	r.Use(middleware.FeatureFlagMiddleware(map[string]bool{
		"personalization": cfg.Features.EnablePersonalization,
		"ab_testing":      cfg.Features.EnableABTesting,
		"tracing":         cfg.Features.EnableTracing,
	}))

	// Health & metrics
	r.GET("/", adminHandler.ServiceInfo)
	r.GET("/health", adminHandler.HealthCheck)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Feed API
	api := r.Group("/api/v1")
	{
		api.GET("/feed", feedHandler.GetFeed)
		api.POST("/interactions/click", feedHandler.RecordClick)
		api.POST("/interactions/order", feedHandler.RecordOrder)
		api.GET("/users/:user_id/analytics", feedHandler.GetUserAnalytics)
	}

	// Experiment API
	experiments := r.Group("/api/v1/experiments")
	{
		experiments.GET("", experimentHandler.ListExperiments)
		experiments.GET("/:id/results", experimentHandler.GetExperimentResults)
		experiments.POST("/significance", experimentHandler.CheckSignificance)
		experiments.POST("/simulate", experimentHandler.SimulateExperiment)
	}

	// AI API
	ai := r.Group("/api/v1/ai")
	{
		ai.POST("/optimize", aiHandler.OptimizeRanking)
		ai.POST("/explain", aiHandler.ExplainRecommendation)
		ai.POST("/batch-optimize", aiHandler.BatchOptimize)
		ai.GET("/health", aiHandler.Health)
	}

	// Admin API
	admin := r.Group("/api/v1/admin")
	{
		admin.GET("/ranking/weights", adminHandler.GetRankingWeights)
		admin.GET("/ranking/variants", adminHandler.ListVariants)
		admin.GET("/metrics-info", adminHandler.GetMetrics)
	}

	// Setup HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Start server in goroutine
	go func() {
		observability.LogInfo("server starting", zap.String("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			observability.LogError("server failed", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	observability.LogInfo("shutting down server...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		observability.LogError("server forced to shutdown", err)
	}

	observability.LogInfo("server exited")
}
