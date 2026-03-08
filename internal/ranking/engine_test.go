package ranking

import (
	"context"
	"testing"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/google/uuid"
)

func TestDefaultScoring(t *testing.T) {
	// Mock repositories would be used here in real tests
	engine := &Engine{}

	venue := domain.Venue{
		ID:          uuid.New(),
		Name:        "Test Venue",
		CuisineType: "burger",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		Rating:      4.5,
		Popularity:  5000,
	}

	userLoc := domain.GeoPoint{Latitude: 40.7130, Longitude: -74.0062}
	weights := domain.RankingWeights{
		Popularity:      0.35,
		Proximity:       0.30,
		Personalization: 0.25,
		Rating:          0.10,
	}

	item, err := engine.DefaultScoring(context.Background(), venue, uuid.New(), userLoc, weights)
	if err != nil {
		t.Fatalf("DefaultScoring failed: %v", err)
	}

	if item.Score <= 0 {
		t.Errorf("Expected positive score, got %f", item.Score)
	}

	if item.PopularityScore < 0 || item.PopularityScore > 1 {
		t.Errorf("PopularityScore should be between 0 and 1, got %f", item.PopularityScore)
	}

	if item.ProximityScore < 0 || item.ProximityScore > 1 {
		t.Errorf("ProximityScore should be between 0 and 1, got %f", item.ProximityScore)
	}
}

func TestGetWeightsForVariant(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		variant string
		wantPop float64
	}{
		{"control", 0.35},
		{"popularity-heavy", 0.55},
		{"personalization-heavy", 0.15},
		{"balanced", 0.30},
		{"rating-focused", 0.20},
		{"unknown", 0.35},
	}

	for _, tt := range tests {
		t.Run(tt.variant, func(t *testing.T) {
			weights := engine.GetWeightsForVariant(tt.variant)
			if weights.Popularity != tt.wantPop {
				t.Errorf("GetWeightsForVariant(%q).Popularity = %f, want %f", tt.variant, weights.Popularity, tt.wantPop)
			}
		})
	}
}

func TestGeoPointDistanceTo(t *testing.T) {
	loc1 := domain.GeoPoint{Latitude: 40.7128, Longitude: -74.0060} // NYC
	loc2 := domain.GeoPoint{Latitude: 40.7580, Longitude: -73.9855} // Midtown

	distance := loc1.DistanceTo(loc2)

	// Distance should be roughly 5.3 km
	if distance < 4 || distance > 7 {
		t.Errorf("Expected distance ~5.3km, got %f", distance)
	}
}

func TestReorderForDiversity(t *testing.T) {
	engine := &Engine{}

	items := []domain.FeedItem{
		{Venue: domain.Venue{CuisineType: "burger"}, Score: 0.9},
		{Venue: domain.Venue{CuisineType: "burger"}, Score: 0.8},
		{Venue: domain.Venue{CuisineType: "burger"}, Score: 0.7},
		{Venue: domain.Venue{CuisineType: "sushi"}, Score: 0.6},
		{Venue: domain.Venue{CuisineType: "pizza"}, Score: 0.5},
	}

	result := engine.ReorderForDiversity(items, 2)

	// First 3 items should not all be burger after re-ordering
	burgerCount := 0
	for i := 0; i < 3 && i < len(result); i++ {
		if result[i].Venue.CuisineType == "burger" {
			burgerCount++
		}
	}

	if burgerCount > 2 {
		t.Errorf("Too many burgers in top 3 after diversity reordering: %d", burgerCount)
	}
}

func TestTimeBasedBoost(t *testing.T) {
	engine := &Engine{}

	breakfastVenue := domain.Venue{CuisineType: "breakfast"}
	dinnerVenue := domain.Venue{CuisineType: "steakhouse"}
	genericVenue := domain.Venue{CuisineType: "burger"}

	_ = engine.getTimeBasedBoost(breakfastVenue)
	_ = engine.getTimeBasedBoost(dinnerVenue)
	_ = engine.getTimeBasedBoost(genericVenue)

	// Just verify no panic occurs - actual boost depends on time of day
}

func BenchmarkDefaultScoring(b *testing.B) {
	engine := &Engine{}
	venue := domain.Venue{
		ID:          uuid.New(),
		Name:        "Benchmark Venue",
		CuisineType: "burger",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		Rating:      4.5,
		Popularity:  5000,
	}
	userLoc := domain.GeoPoint{Latitude: 40.7130, Longitude: -74.0062}
	weights := domain.RankingWeights{
		Popularity: 0.35, Proximity: 0.30,
		Personalization: 0.25, Rating: 0.10,
	}
	userID := uuid.New()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.DefaultScoring(ctx, venue, userID, userLoc, weights)
	}
}
