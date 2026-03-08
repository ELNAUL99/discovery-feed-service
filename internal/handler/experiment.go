package handler

import (
	"net/http"

	"github.com/discovery-feed-service/internal/experiment"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ExperimentHandler struct {
	service *experiment.Service
}

func NewExperimentHandler(service *experiment.Service) *ExperimentHandler {
	return &ExperimentHandler{service: service}
}

func (h *ExperimentHandler) ListExperiments(c *gin.Context) {
	ctx := c.Request.Context()

	experiments, err := h.service.GetActiveExperiments(ctx)
	if err != nil {
		observability.LogError("failed to list experiments", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list experiments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"experiments": experiments,
		"count":       len(experiments),
	})
}

func (h *ExperimentHandler) GetExperimentResults(c *gin.Context) {
	ctx := c.Request.Context()
	experimentID := c.Param("id")

	id, err := uuid.Parse(experimentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid experiment id"})
		return
	}

	results, err := h.service.GetExperimentResults(ctx, id)
	if err != nil {
		observability.LogError("failed to get experiment results", err,
			zap.String("experiment_id", experimentID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get results"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"experiment_id": experimentID,
		"metrics":       results,
	})
}

func (h *ExperimentHandler) CheckSignificance(c *gin.Context) {
	var req struct {
		ControlConversions   int `json:"control_conversions"`
		ControlTotal         int `json:"control_total"`
		VariantConversions   int `json:"variant_conversions"`
		VariantTotal         int `json:"variant_total"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	zScore, significant := h.service.CheckStatisticalSignificance(
		req.ControlConversions, req.ControlTotal,
		req.VariantConversions, req.VariantTotal,
	)

	c.JSON(http.StatusOK, gin.H{
		"z_score":      zScore,
		"significant":  significant,
		"p_value_est":  estimatePValue(zScore),
		"control_rate": float64(req.ControlConversions) / float64(req.ControlTotal),
		"variant_rate": float64(req.VariantConversions) / float64(req.VariantTotal),
	})
}

func (h *ExperimentHandler) SimulateExperiment(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		BaselineCTR   float64 `json:"baseline_ctr"`
		ExpectedCTR   float64 `json:"expected_ctr"`
		DailyTraffic  int     `json:"daily_traffic"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	days, confidence := h.service.SimulateExperiment(ctx, req.BaselineCTR, req.ExpectedCTR, req.DailyTraffic)

	c.JSON(http.StatusOK, gin.H{
		"days_required":    days,
		"confidence":       confidence,
		"sample_size":        h.service.CalculateSampleSize(req.BaselineCTR, req.ExpectedCTR-req.BaselineCTR, 0.8, 0.05),
		"baseline_ctr":       req.BaselineCTR,
		"expected_ctr":       req.ExpectedCTR,
		"daily_traffic":      req.DailyTraffic,
	})
}

func estimatePValue(zScore float64) float64 {
	// Rough estimation for |z| > 1
	if zScore == 0 {
		return 1.0
	}
	absZ := zScore
	if absZ < 0 {
		absZ = -absZ
	}
	// Approximate using tail bound
	return 2.0 * 0.5 * (1.0 - absZ/(1.0+absZ))
}
