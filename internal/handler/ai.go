package handler

import (
	"net/http"

	"github.com/discovery-feed-service/internal/ai"
	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AIHandler struct {
	agent *ai.Agent
}

func NewAIHandler(agent *ai.Agent) *AIHandler {
	return &AIHandler{agent: agent}
}

func (h *AIHandler) OptimizeRanking(c *gin.Context) {
	ctx := c.Request.Context()

	var req ai.OptimizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Goal == "" {
		req.Goal = "improve_engagement"
	}

	if req.CurrentConfig.Popularity == 0 && req.CurrentConfig.Proximity == 0 {
		// Use default weights if not provided
		req.CurrentConfig = domain.RankingWeights{
			Popularity:      0.35,
			Proximity:       0.30,
			Personalization: 0.25,
			Rating:          0.10,
			PriceMatch:      0.00,
		}
	}

	result, err := h.agent.OptimizeRanking(ctx, req)
	if err != nil {
		observability.LogError("AI optimization failed", err,
			zap.String("goal", req.Goal))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "optimization failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"recommendation": result.Recommendation,
		"simulation":     result.Simulation,
		"pr_diff":        result.PRDiff,
	})
}

func (h *AIHandler) ExplainRecommendation(c *gin.Context) {
	ctx := c.Request.Context()

	var rec domain.AIRecommendation
	if err := c.ShouldBindJSON(&rec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	explanation, err := h.agent.ExplainRecommendation(ctx, rec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"explanation": explanation,
	})
}

func (h *AIHandler) BatchOptimize(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		Configs []domain.RankingConfig `json:"configs"`
		Goal    string                 `json:"goal"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results, err := h.agent.BatchOptimize(ctx, req.Configs, req.Goal)
	if err != nil {
		observability.LogError("batch optimization failed", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "batch optimization failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"count":   len(results),
	})
}

func (h *AIHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"ai_enabled": true,
		"mock_mode": true,
		"version":   "1.0.0",
	})
}
