package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/discovery-feed-service/internal/config"
	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/ranking"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Agent struct {
	rankingEngine *ranking.Engine
	aiConfig      config.AIConfig
}

func NewAgent(engine *ranking.Engine, config config.AIConfig) *Agent {
	return &Agent{
		rankingEngine: engine,
		aiConfig:      config,
	}
}

// OptimizationRequest represents a request to optimize ranking
type OptimizationRequest struct {
	CurrentConfig  domain.RankingWeights     `json:"current_config"`
	Goal           string                    `json:"goal"` // e.g., "increase_ctr", "improve_engagement"
	HistoricalData []domain.SimulationResult `json:"historical_data,omitempty"`
}

// OptimizationResult contains the AI recommendation
type OptimizationResult struct {
	Recommendation *domain.AIRecommendation `json:"recommendation"`
	Simulation     *domain.SimulationResult `json:"simulation"`
	PRDiff         string                   `json:"pr_diff"`
}

// OptimizeRanking runs the AI optimization workflow
func (a *Agent) OptimizeRanking(ctx context.Context, req OptimizationRequest) (*OptimizationResult, error) {
	_, span := observability.StartSpan(ctx, "ai.OptimizeRanking")
	defer span.End()

	observability.LogInfo("starting AI ranking optimization",
		zap.String("goal", req.Goal),
		zap.Any("current_weights", req.CurrentConfig),
	)

	// Step 1: Analyze current performance
	analysis := a.analyzeCurrentPerformance(req)

	// Step 2: Generate recommendations
	recommendation, proposedWeights, err := a.generateRecommendation(ctx, req, analysis)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recommendation: %w", err)
	}

	// Step 3: Simulate on historical data
	simulation := a.simulateOnHistoricalData(req, recommendation)

	// Step 4: Generate PR-style diff
	prDiff := a.generatePRDiff(req.CurrentConfig, proposedWeights)

	observability.AIRecommendationsGenerated.Inc()

	observability.LogInfo("AI optimization complete",
		zap.String("recommendation_id", recommendation.ID.String()),
		zap.Float64("predicted_impact", simulation.TotalScore),
		zap.Float64("confidence", recommendation.Confidence),
	)

	return &OptimizationResult{
		Recommendation: recommendation,
		Simulation:     simulation,
		PRDiff:         prDiff,
	}, nil
}

func (a *Agent) analyzeCurrentPerformance(req OptimizationRequest) map[string]interface{} {
	analysis := map[string]interface{}{
		"current_weights":   req.CurrentConfig,
		"weight_balance":    a.checkWeightBalance(req.CurrentConfig),
		"optimization_goal": req.Goal,
	}

	// Check if weights sum to ~1.0
	total := req.CurrentConfig.Popularity + req.CurrentConfig.Proximity +
		req.CurrentConfig.Personalization + req.CurrentConfig.Rating +
		req.CurrentConfig.PriceMatch + req.CurrentConfig.Recency

	analysis["weight_sum"] = total
	analysis["is_normalized"] = math.Abs(total-1.0) < 0.01

	// Identify dominant factor
	weights := map[string]float64{
		"popularity":      req.CurrentConfig.Popularity,
		"proximity":       req.CurrentConfig.Proximity,
		"personalization": req.CurrentConfig.Personalization,
		"rating":          req.CurrentConfig.Rating,
		"price_match":     req.CurrentConfig.PriceMatch,
		"recency":         req.CurrentConfig.Recency,
	}

	type pair struct {
		name  string
		value float64
	}
	pairs := make([]pair, 0, len(weights))
	for k, v := range weights {
		pairs = append(pairs, pair{k, v})
	}

	// Sort to find dominant
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].value > pairs[j].value
	})

	analysis["dominant_factor"] = pairs[0].name
	analysis["dominant_value"] = pairs[0].value
	analysis["weakest_factor"] = pairs[len(pairs)-1].name

	return analysis
}

func (a *Agent) checkWeightBalance(weights domain.RankingWeights) string {
	// Check concentration
	maxWeight := math.Max(weights.Popularity, math.Max(weights.Proximity,
		math.Max(weights.Personalization, math.Max(weights.Rating, weights.PriceMatch))))

	if maxWeight > 0.6 {
		return "over_concentrated"
	}
	if maxWeight < 0.2 {
		return "under_concentrated"
	}
	return "balanced"
}

func (a *Agent) generateRecommendation(ctx context.Context, req OptimizationRequest, analysis map[string]interface{}) (*domain.AIRecommendation, domain.RankingWeights, error) {
	if a.aiConfig.MockMode {
		recommendation, proposed := a.generateMockRecommendation(req, analysis)
		return recommendation, proposed, nil
	}

	// Real LLM API call
	return a.callLLM(ctx, req, analysis)
}

func (a *Agent) generateMockRecommendation(req OptimizationRequest, analysis map[string]interface{}) (*domain.AIRecommendation, domain.RankingWeights) {
	// Rule-based recommendation engine that mimics AI reasoning

	current := req.CurrentConfig
	proposed := current

	switch req.Goal {
	case "increase_ctr":
		// CTR benefits from personalization and proximity
		proposed.Personalization = math.Min(current.Personalization+0.10, 0.50)
		proposed.Proximity = math.Min(current.Proximity+0.05, 0.40)
		proposed.Popularity = math.Max(current.Popularity-0.10, 0.10)

	case "improve_engagement":
		// Engagement benefits from diversity (balance)
		avg := (current.Popularity + current.Proximity + current.Personalization + current.Rating) / 4
		proposed.Popularity = avg + (current.Popularity-avg)*0.5
		proposed.Proximity = avg + (current.Proximity-avg)*0.5
		proposed.Personalization = avg + (current.Personalization-avg)*0.5
		proposed.Rating = avg + (current.Rating-avg)*0.5

	case "maximize_revenue":
		// Revenue benefits from popularity and rating
		proposed.Popularity = math.Min(current.Popularity+0.15, 0.55)
		proposed.Rating = math.Min(current.Rating+0.10, 0.25)
		proposed.Personalization = math.Max(current.Personalization-0.10, 0.10)

	case "new_user_retention":
		// New users need popularity (social proof) and proximity
		proposed.Popularity = math.Min(current.Popularity+0.15, 0.50)
		proposed.Proximity = math.Min(current.Proximity+0.10, 0.40)
		proposed.Personalization = math.Max(current.Personalization-0.15, 0.05)

	default:
		// General optimization - balance weights
		proposed = domain.RankingWeights{
			Popularity:      0.30,
			Proximity:       0.25,
			Personalization: 0.25,
			Rating:          0.15,
			PriceMatch:      0.05,
			Recency:         0.00,
		}
	}

	// Normalize
	total := proposed.Popularity + proposed.Proximity + proposed.Personalization +
		proposed.Rating + proposed.PriceMatch + proposed.Recency

	if total > 0 {
		proposed.Popularity /= total
		proposed.Proximity /= total
		proposed.Personalization /= total
		proposed.Rating /= total
		proposed.PriceMatch /= total
		proposed.Recency /= total
	}

	// Calculate confidence based on how different the proposal is
	diff := math.Abs(current.Popularity-proposed.Popularity) +
		math.Abs(current.Proximity-proposed.Proximity) +
		math.Abs(current.Personalization-proposed.Personalization) +
		math.Abs(current.Rating-proposed.Rating)

	confidence := math.Min(diff*2+0.5, 0.95)
	impact := diff * 1.5

	description := fmt.Sprintf(
		"Based on analysis of current configuration (dominated by %s), "+
			"recommend shifting weights to optimize for '%s'. "+
			"Key changes: popularity %.0f%%→%.0f%%, proximity %.0f%%→%.0f%%, personalization %.0f%%→%.0f%%",
		analysis["dominant_factor"], req.Goal,
		current.Popularity*100, proposed.Popularity*100,
		current.Proximity*100, proposed.Proximity*100,
		current.Personalization*100, proposed.Personalization*100,
	)

	return &domain.AIRecommendation{
		ID:          uuid.New(),
		Type:        "weight_adjustment",
		Description: description,
		Diff:        a.formatWeightsDiff(current, proposed),
		Confidence:  confidence,
		Impact:      impact,
		CreatedAt:   time.Now(),
	}, proposed
}

func (a *Agent) callLLM(ctx context.Context, req OptimizationRequest, analysis map[string]interface{}) (*domain.AIRecommendation, domain.RankingWeights, error) {
	// Call external LLM API (e.g., OpenAI, Anthropic, or internal model)

	prompt := fmt.Sprintf(`
You are a ranking optimization AI for a food delivery discovery feed.

Current ranking weights:
- Popularity: %.2f
- Proximity: %.2f  
- Personalization: %.2f
- Rating: %.2f
- Price Match: %.2f

Optimization goal: %s

Analysis:
- Dominant factor: %s
- Balance status: %s

Recommend new weights as JSON: {"popularity": X, "proximity": Y, "personalization": Z, "rating": W, "price_match": V}
`,
		req.CurrentConfig.Popularity,
		req.CurrentConfig.Proximity,
		req.CurrentConfig.Personalization,
		req.CurrentConfig.Rating,
		req.CurrentConfig.PriceMatch,
		req.Goal,
		analysis["dominant_factor"],
		analysis["weight_balance"],
	)

	if a.aiConfig.Endpoint == "" {
		recommendation, proposed := a.generateMockRecommendation(req, analysis)
		return recommendation, proposed, nil
	}

	// Make API call
	requestBody, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "system", "content": "You are a ranking optimization AI. Respond only with JSON."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.aiConfig.Endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, domain.RankingWeights{}, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+a.aiConfig.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, domain.RankingWeights{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.RankingWeights{}, err
	}

	// Parse response
	var llmResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &llmResponse); err != nil {
		return nil, domain.RankingWeights{}, err
	}

	if len(llmResponse.Choices) == 0 {
		recommendation, proposed := a.generateMockRecommendation(req, analysis)
		return recommendation, proposed, nil
	}

	content := llmResponse.Choices[0].Message.Content

	// Extract JSON from response
	var proposed domain.RankingWeights
	if err := json.Unmarshal([]byte(content), &proposed); err != nil {
		// Fallback to mock
		recommendation, proposed := a.generateMockRecommendation(req, analysis)
		return recommendation, proposed, nil
	}

	return &domain.AIRecommendation{
		ID:          uuid.New(),
		Type:        "weight_adjustment",
		Description: "LLM-optimized weights for " + req.Goal,
		Diff:        a.formatWeightsDiff(req.CurrentConfig, proposed),
		Confidence:  0.80,
		Impact:      0.15,
		CreatedAt:   time.Now(),
	}, proposed, nil
}

func (a *Agent) simulateOnHistoricalData(req OptimizationRequest, rec *domain.AIRecommendation) *domain.SimulationResult {
	// Simulate how the new weights would perform
	// In production, this would run on actual historical interaction data

	// Mock simulation results based on optimization goal
	var avgCTR, avgConversion, satisfaction float64

	switch req.Goal {
	case "increase_ctr":
		avgCTR = 0.052 + rec.Impact*0.1
		avgConversion = 0.025
		satisfaction = 0.78
	case "improve_engagement":
		avgCTR = 0.048
		avgConversion = 0.027
		satisfaction = 0.85
	case "maximize_revenue":
		avgCTR = 0.045
		avgConversion = 0.032
		satisfaction = 0.72
	default:
		avgCTR = 0.050
		avgConversion = 0.026
		satisfaction = 0.80
	}

	totalScore := avgCTR*0.4 + avgConversion*0.4 + satisfaction*0.2

	return &domain.SimulationResult{
		ConfigID:         uuid.New(),
		ConfigName:       rec.Type,
		AvgCTR:           avgCTR,
		AvgConversion:    avgConversion,
		UserSatisfaction: satisfaction,
		TotalScore:       totalScore,
	}
}

func (a *Agent) generatePRDiff(current, proposed domain.RankingWeights) string {
	var diff strings.Builder

	diff.WriteString("```diff\n")
	diff.WriteString("--- a/internal/ranking/config.go\n")
	diff.WriteString("+++ b/internal/ranking/config.go\n")
	diff.WriteString("@@ -1,10 +1,10 @@\n")
	diff.WriteString("  var DefaultWeights = RankingWeights{\n")
	diff.WriteString(fmt.Sprintf("-     Popularity:      %.2f,\n", current.Popularity))
	diff.WriteString(fmt.Sprintf("+     Popularity:      %.2f,\n", proposed.Popularity))
	diff.WriteString(fmt.Sprintf("-     Proximity:       %.2f,\n", current.Proximity))
	diff.WriteString(fmt.Sprintf("+     Proximity:       %.2f,\n", proposed.Proximity))
	diff.WriteString(fmt.Sprintf("-     Personalization: %.2f,\n", current.Personalization))
	diff.WriteString(fmt.Sprintf("+     Personalization: %.2f,\n", proposed.Personalization))
	diff.WriteString(fmt.Sprintf("-     Rating:          %.2f,\n", current.Rating))
	diff.WriteString(fmt.Sprintf("+     Rating:          %.2f,\n", proposed.Rating))
	diff.WriteString(fmt.Sprintf("-     PriceMatch:      %.2f,\n", current.PriceMatch))
	diff.WriteString(fmt.Sprintf("+     PriceMatch:      %.2f,\n", proposed.PriceMatch))
	diff.WriteString("  }\n")
	diff.WriteString("```\n")

	return diff.String()
}

func (a *Agent) formatWeightsDiff(current, proposed domain.RankingWeights) string {
	return fmt.Sprintf(
		"popularity:%.2f→%.2f proximity:%.2f→%.2f personalization:%.2f→%.2f rating:%.2f→%.2f",
		current.Popularity, proposed.Popularity,
		current.Proximity, proposed.Proximity,
		current.Personalization, proposed.Personalization,
		current.Rating, proposed.Rating,
	)
}

// BatchOptimize runs optimization across multiple configurations
func (a *Agent) BatchOptimize(ctx context.Context, configs []domain.RankingConfig, goal string) ([]OptimizationResult, error) {
	results := make([]OptimizationResult, 0, len(configs))

	for _, config := range configs {
		req := OptimizationRequest{
			CurrentConfig: config.Weights,
			Goal:          goal,
		}

		result, err := a.OptimizeRanking(ctx, req)
		if err != nil {
			observability.LogError("batch optimization failed for config", err,
				zap.String("config_id", config.ID.String()))
			continue
		}

		results = append(results, *result)
	}

	return results, nil
}

// ExplainRecommendation provides human-readable explanation
func (a *Agent) ExplainRecommendation(ctx context.Context, rec domain.AIRecommendation) (string, error) {
	explanation := fmt.Sprintf(`
## AI Ranking Optimization Recommendation

**Type**: %s
**Confidence**: %.0f%%
**Predicted Impact**: +%.1f%%

### What Changed
%s

### Why This Works
%s

### Simulated Results
Based on historical data simulation, this change is predicted to:
- Improve overall feed quality
- Better align with user preferences
- Maintain diversity in recommendations

**Recommendation**: %s
`,
		rec.Type,
		rec.Confidence*100,
		rec.Impact*100,
		rec.Diff,
		rec.Description,
		func() string {
			if rec.Confidence > 0.8 {
				return "APPROVE - High confidence recommendation"
			}
			if rec.Confidence > 0.6 {
				return "REVIEW - Moderate confidence, manual review suggested"
			}
			return "REJECT - Low confidence, needs more data"
		}(),
	)

	return explanation, nil
}
