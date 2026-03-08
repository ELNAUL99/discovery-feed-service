package handler

import (
	"net/http"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type FeedHandler struct {
	feedService *service.FeedService
}

func NewFeedHandler(feedService *service.FeedService) *FeedHandler {
	return &FeedHandler{feedService: feedService}
}

func (h *FeedHandler) GetFeed(c *gin.Context) {
	ctx := c.Request.Context()

	var req domain.FeedRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	response, err := h.feedService.GetFeed(ctx, req)
	if err != nil {
		observability.LogError("failed to get feed", err,
			zap.String("user_id", req.UserID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate feed"})
		return
	}

	c.Header("X-Request-ID", response.RequestID)
	c.Header("X-Experiment-Variant", response.Variant)
	c.JSON(http.StatusOK, response)
}

func (h *FeedHandler) RecordClick(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		UserID  string `json:"user_id" binding:"required"`
		VenueID string `json:"venue_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.feedService.RecordClick(ctx, req.UserID, req.VenueID); err != nil {
		observability.LogError("failed to record click", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record click"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "recorded"})
}

func (h *FeedHandler) RecordOrder(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		UserID     string  `json:"user_id" binding:"required"`
		VenueID    string  `json:"venue_id" binding:"required"`
		OrderValue float64 `json:"order_value"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.feedService.RecordOrder(ctx, req.UserID, req.VenueID, req.OrderValue); err != nil {
		observability.LogError("failed to record order", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record order"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "recorded"})
}

func (h *FeedHandler) GetUserAnalytics(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.Param("user_id")

	if _, err := uuid.Parse(userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	analytics, err := h.feedService.GetUserFeedAnalytics(ctx, userID)
	if err != nil {
		observability.LogError("failed to get user analytics", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get analytics"})
		return
	}

	c.JSON(http.StatusOK, analytics)
}
