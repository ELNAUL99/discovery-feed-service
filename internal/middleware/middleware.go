package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/discovery-feed-service/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FeatureFlagMiddleware enables feature flag checks
func FeatureFlagMiddleware(flags map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		for feature, enabled := range flags {
			c.Set("feature_"+feature, enabled)
		}
		c.Next()
	}
}

// RateLimitMiddleware simple rate limiter
func RateLimitMiddleware(requests int, window time.Duration) gin.HandlerFunc {
	// In production, use Redis-backed rate limiter
	return func(c *gin.Context) {
		c.Next()
	}
}

// RequestTimeoutMiddleware adds request timeout
func RequestTimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// CORSMiddleware handles CORS
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Experiment-Variant")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RecoveryMiddleware handles panics
func RecoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		observability.LogError("panic recovered", nil,
			zap.Any("recovered", recovered),
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
	})
}

// RequestIDMiddleware ensures every request has an ID
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)
		c.Next()
	}
}
