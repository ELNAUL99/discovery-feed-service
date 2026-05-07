package ai

import (
	"context"
	"testing"

	"github.com/discovery-feed-service/internal/config"
	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/ranking"
)

func TestOptimizeRanking(t *testing.T) {
	engine := &ranking.Engine{}
	cfg := config.AIConfig{
		Enabled:  true,
		MockMode: true,
	}
	agent := NewAgent(engine, cfg)

	req := OptimizationRequest{
		CurrentConfig: domain.RankingWeights{
			Popularity:      0.35,
			Proximity:       0.30,
			Personalization: 0.25,
			Rating:          0.10,
		},
		Goal: "increase_ctr",
	}

	result, err := agent.OptimizeRanking(context.Background(), req)
	if err != nil {
		t.Fatalf("OptimizeRanking failed: %v", err)
	}

	if result.Recommendation == nil {
		t.Fatal("Expected recommendation, got nil")
	}

	if result.Recommendation.Confidence <= 0 || result.Recommendation.Confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", result.Recommendation.Confidence)
	}

	if result.Simulation == nil {
		t.Fatal("Expected simulation result, got nil")
	}

	if result.PRDiff == "" {
		t.Error("Expected PR diff, got empty string")
	}
}

func TestGenerateMockRecommendation(t *testing.T) {
	engine := &ranking.Engine{}
	agent := NewAgent(engine, config.AIConfig{MockMode: true})

	tests := []struct {
		goal       string
		wantPopInc bool
	}{
		{"increase_ctr", false},       // Should decrease popularity
		{"improve_engagement", false}, // Should balance
		{"maximize_revenue", true},    // Should increase popularity
		{"new_user_retention", true},  // Should increase popularity
		{"", false},                   // Default
	}

	for _, tt := range tests {
		t.Run(tt.goal, func(t *testing.T) {
			req := OptimizationRequest{
				CurrentConfig: domain.RankingWeights{
					Popularity: 0.35, Proximity: 0.30,
					Personalization: 0.25, Rating: 0.10,
				},
				Goal: tt.goal,
			}

			result, _ := agent.OptimizeRanking(context.Background(), req)
			rec := result.Recommendation

			if rec == nil {
				t.Fatal("Expected recommendation")
			}

			if rec.Type != "weight_adjustment" {
				t.Errorf("Expected type 'weight_adjustment', got %s", rec.Type)
			}
		})
	}
}

func TestExplainRecommendation(t *testing.T) {
	engine := &ranking.Engine{}
	agent := NewAgent(engine, config.AIConfig{MockMode: true})

	rec := domain.AIRecommendation{
		Type:        "weight_adjustment",
		Description: "Test recommendation",
		Diff:        "popularity:0.35→0.45",
		Confidence:  0.85,
		Impact:      0.15,
	}

	explanation, err := agent.ExplainRecommendation(context.Background(), rec)
	if err != nil {
		t.Fatalf("ExplainRecommendation failed: %v", err)
	}

	if explanation == "" {
		t.Error("Expected non-empty explanation")
	}

	if !contains(explanation, "APPROVE") {
		t.Error("Expected explanation to include approval recommendation")
	}
}

func TestCheckWeightBalance(t *testing.T) {
	engine := &ranking.Engine{}
	agent := NewAgent(engine, config.AIConfig{MockMode: true})

	tests := []struct {
		name string
		pop  float64
		prox float64
		pers float64
		want string
	}{
		{"balanced", 0.30, 0.30, 0.25, "balanced"},
		{"over_concentrated", 0.70, 0.10, 0.10, "over_concentrated"},
		{"under_concentrated", 0.10, 0.10, 0.10, "under_concentrated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weights := domain.RankingWeights{
				Popularity: tt.pop, Proximity: tt.prox,
				Personalization: tt.pers, Rating: 0.10,
			}
			result := agent.checkWeightBalance(weights)
			if result != tt.want {
				t.Errorf("checkWeightBalance() = %q, want %q", result, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && containsSub(s, substr)))
}

func containsSub(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
