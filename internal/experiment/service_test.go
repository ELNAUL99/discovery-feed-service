package experiment

import (
	"context"
	"testing"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/google/uuid"
)

func TestHashUserExperiment(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	expID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	hash1 := hashUserExperiment(userID, expID)
	hash2 := hashUserExperiment(userID, expID)

	if hash1 != hash2 {
		t.Error("Hash should be deterministic for same inputs")
	}

	// Different user should give different hash
	otherUser := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	hash3 := hashUserExperiment(otherUser, expID)
	if hash1 == hash3 {
		t.Error("Different users should produce different hashes")
	}
}

func TestCalculateSampleSize(t *testing.T) {
	svc := &Service{}

	// Typical A/B test: baseline 5%, MDE 1%, 80% power, 5% alpha
	size := svc.CalculateSampleSize(0.05, 0.01, 0.8, 0.05)
	if size <= 0 {
		t.Errorf("Expected positive sample size, got %d", size)
	}

	// Should be roughly 6,000+ for these parameters
	if size < 1000 {
		t.Errorf("Sample size seems too small: %d", size)
	}
}

func TestCheckStatisticalSignificance(t *testing.T) {
	svc := &Service{}

	tests := []struct {
		name       string
		controlC   int
		controlT   int
		variantC   int
		variantT   int
		wantSig    bool
	}{
		{
			name:     "significant improvement",
			controlC: 50, controlT: 1000,
			variantC: 80, variantT: 1000,
			wantSig: true,
		},
		{
			name:     "not significant",
			controlC: 50, controlT: 1000,
			variantC: 55, variantT: 1000,
			wantSig: false,
		},
		{
			name:     "no data",
			controlC: 0, controlT: 0,
			variantC: 0, variantT: 1000,
			wantSig: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zScore, sig := svc.CheckStatisticalSignificance(tt.controlC, tt.controlT, tt.variantC, tt.variantT)
			if sig != tt.wantSig {
				t.Errorf("CheckStatisticalSignificance() significant=%v, zScore=%f, want significant=%v",
					sig, zScore, tt.wantSig)
			}
		})
	}
}

func TestGetVariantConfig(t *testing.T) {
	svc := &Service{}

	variant := &domain.Variant{
		Name: "popularity-heavy",
	}

	weights := svc.GetVariantConfig(variant)
	if weights.Popularity != 0.55 {
		t.Errorf("Expected popularity weight 0.55 for 'popularity-heavy', got %f", weights.Popularity)
	}
}

func TestSimulateExperiment(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()

	days, confidence := svc.SimulateExperiment(ctx, 0.05, 0.06, 500)

	if days <= 0 {
		t.Errorf("Expected positive days, got %d", days)
	}

	if confidence < 0 || confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", confidence)
	}
}
