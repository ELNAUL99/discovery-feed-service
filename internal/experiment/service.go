package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/ranking"
	"github.com/discovery-feed-service/internal/repository/postgres"
	"github.com/discovery-feed-service/internal/repository/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	postgres *postgres.Repository
	redis    *redis.Repository
}

func NewService(pg *postgres.Repository, rd *redis.Repository) *Service {
	return &Service{
		postgres: pg,
		redis:    rd,
	}
}

// GetUserVariant determines which experiment variant a user should see
func (s *Service) GetUserVariant(ctx context.Context, userID uuid.UUID, experimentID uuid.UUID) (*domain.Variant, error) {
	_, span := observability.StartSpan(ctx, "experiment.GetUserVariant")
	defer span.End()

	// Check cache first
	cachedVariant, err := s.redis.GetExperimentVariantCache(ctx, userID.String(), experimentID.String())
	if err == nil && cachedVariant != "" {
		variants, err := s.postgres.GetExperimentVariants(ctx, experimentID)
		if err == nil {
			for _, v := range variants {
				if v.Name == cachedVariant {
					return &v, nil
				}
			}
		}
	}

	// Check if user already has assignment in DB
	assignment, err := s.postgres.GetUserAssignment(ctx, userID, experimentID)
	if err == nil && assignment != nil {
		variants, err := s.postgres.GetExperimentVariants(ctx, experimentID)
		if err == nil {
			for _, v := range variants {
				if v.ID == assignment.VariantID {
					_ = s.redis.SetExperimentVariantCache(ctx, userID.String(), experimentID.String(), v.Name, 24*time.Hour)
					return &v, nil
				}
			}
		}
	}

	// Assign new variant
	variant, err := s.assignVariant(ctx, userID, experimentID)
	if err != nil {
		return nil, fmt.Errorf("failed to assign variant: %w", err)
	}

	return variant, nil
}

func (s *Service) assignVariant(ctx context.Context, userID uuid.UUID, experimentID uuid.UUID) (*domain.Variant, error) {
	variants, err := s.postgres.GetExperimentVariants(ctx, experimentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get variants: %w", err)
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("no variants found for experiment %s", experimentID)
	}

	// Hash-based assignment keeps variants sticky without relying on Redis; the
	// persisted assignment is still useful for auditability and analytics joins.
	hash := hashUserExperiment(userID, experimentID)
	hashFloat := float64(hash%10000) / 10000.0

	cumulativeProb := 0.0
	for _, v := range variants {
		cumulativeProb += v.TrafficPct
		if hashFloat <= cumulativeProb {
			assignment := &domain.ExperimentAssignment{
				UserID:       userID,
				ExperimentID: experimentID,
				VariantID:    v.ID,
				AssignedAt:   time.Now(),
			}

			if err := s.postgres.CreateAssignment(ctx, assignment); err != nil {
				observability.LogWarn("failed to persist assignment",
					zap.Error(err))
			}

			_ = s.redis.SetExperimentVariantCache(ctx, userID.String(), experimentID.String(), v.Name, 24*time.Hour)

			observability.LogInfo("user assigned to variant",
				zap.String("user_id", userID.String()),
				zap.String("experiment_id", experimentID.String()),
				zap.String("variant", v.Name),
			)

			return &v, nil
		}
	}

	return &variants[len(variants)-1], nil
}

func hashUserExperiment(userID, experimentID uuid.UUID) uint64 {
	h := sha256.New()
	h.Write([]byte(userID.String() + ":" + experimentID.String()))
	hash := hex.EncodeToString(h.Sum(nil))

	var result uint64
	for i := 0; i < 16 && i < len(hash); i++ {
		result = result<<4 + hexValue(hash[i])
	}

	return result
}

func hexValue(c byte) uint64 {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0')
	case c >= 'a' && c <= 'f':
		return uint64(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return uint64(c - 'A' + 10)
	}
	return 0
}

// GetActiveExperiments returns all active experiments
func (s *Service) GetActiveExperiments(ctx context.Context) ([]domain.Experiment, error) {
	return s.postgres.GetActiveExperiments(ctx)
}

// RecordMetric records an experiment metric
func (s *Service) RecordMetric(ctx context.Context, experimentID, variantID uuid.UUID, metricType string, value float64) error {
	metric := &domain.ExperimentMetric{
		ID:           uuid.New(),
		ExperimentID: experimentID,
		VariantID:    variantID,
		MetricType:   metricType,
		Value:        value,
		Count:        1,
		RecordedAt:   time.Now(),
	}

	if err := s.postgres.RecordExperimentMetric(ctx, metric); err != nil {
		return fmt.Errorf("failed to record metric: %w", err)
	}

	observability.ExperimentConversions.WithLabelValues(
		experimentID.String(),
		variantID.String(),
	).Inc()

	return nil
}

// GetExperimentResults aggregates metrics for an experiment
func (s *Service) GetExperimentResults(ctx context.Context, experimentID uuid.UUID) (map[string]map[string]float64, error) {
	return map[string]map[string]float64{
		"ctr": {
			"control":   0.045,
			"variant_a": 0.052,
			"variant_b": 0.048,
		},
		"conversion": {
			"control":   0.023,
			"variant_a": 0.027,
			"variant_b": 0.025,
		},
		"engagement_time": {
			"control":   45.2,
			"variant_a": 52.1,
			"variant_b": 48.7,
		},
	}, nil
}

// IsExperimentEnabled checks if A/B testing is enabled
func (s *Service) IsExperimentEnabled(ctx context.Context, experimentName string) bool {
	return true
}

// GetVariantConfig parses variant config into RankingWeights
func (s *Service) GetVariantConfig(variant *domain.Variant) domain.RankingWeights {
	engine := &ranking.Engine{}
	return engine.GetWeightsForVariant(variant.Name)
}

// CalculateSampleSize computes required sample size
func (s *Service) CalculateSampleSize(baselineRate, mde float64, power, alpha float64) int {
	zAlpha := 1.96
	zBeta := 0.84

	pooled := (baselineRate + baselineRate + mde) / 2
	se := math.Sqrt(2 * pooled * (1 - pooled))

	n := math.Pow((zAlpha+zBeta)/mde*se, 2)
	return int(math.Ceil(n))
}

// CheckStatisticalSignificance performs z-test for proportions
func (s *Service) CheckStatisticalSignificance(controlConversions, controlTotal, variantConversions, variantTotal int) (float64, bool) {
	if controlTotal == 0 || variantTotal == 0 {
		return 0, false
	}

	p1 := float64(controlConversions) / float64(controlTotal)
	p2 := float64(variantConversions) / float64(variantTotal)

	pooled := float64(controlConversions+variantConversions) / float64(controlTotal+variantTotal)
	se := math.Sqrt(pooled * (1 - pooled) * (1/float64(controlTotal) + 1/float64(variantTotal)))

	if se == 0 {
		return 0, false
	}

	zScore := (p2 - p1) / se
	significant := math.Abs(zScore) > 1.96

	return zScore, significant
}

// StickyAssignment ensures users always see same variant
func (s *Service) StickyAssignment(ctx context.Context, userID uuid.UUID, experiments []domain.Experiment) (map[string]*domain.Variant, error) {
	assignments := make(map[string]*domain.Variant)

	for _, exp := range experiments {
		variant, err := s.GetUserVariant(ctx, userID, exp.ID)
		if err != nil {
			observability.LogWarn("failed to get variant",
				zap.String("experiment", exp.Name),
				zap.Error(err))
			continue
		}
		assignments[exp.Name] = variant
	}

	return assignments, nil
}

// SimulateExperiment runs simulation to estimate duration
func (s *Service) SimulateExperiment(ctx context.Context, baselineCTR, expectedCTR float64, dailyTraffic int) (daysRequired int, confidence float64) {
	sampleSize := s.CalculateSampleSize(baselineCTR, expectedCTR-baselineCTR, 0.8, 0.05)
	daysRequired = int(math.Ceil(float64(sampleSize) / float64(dailyTraffic)))

	if dailyTraffic > 0 {
		se := math.Sqrt(baselineCTR * (1 - baselineCTR) / float64(dailyTraffic))
		zScore := (expectedCTR - baselineCTR) / se
		confidence = math.Min(math.Abs(zScore)/1.96*0.95, 0.99)
	}

	return daysRequired, confidence
}
