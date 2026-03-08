package ranking

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/repository/postgres"
	"github.com/discovery-feed-service/internal/repository/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Engine struct {
	postgres *postgres.Repository
	redis    *redis.Repository
}

func NewEngine(pg *postgres.Repository, rd *redis.Repository) *Engine {
	return &Engine{
		postgres: pg,
		redis:    rd,
	}
}

// ScoringStrategy lets experiments swap the scoring model without changing the
// feed orchestration path, which keeps routing, caching, and observability stable.
type ScoringStrategy func(ctx context.Context, venue domain.Venue, userID uuid.UUID, userLoc domain.GeoPoint, weights domain.RankingWeights) (domain.FeedItem, error)

func (e *Engine) ScoreVenues(ctx context.Context, venues []domain.Venue, userID uuid.UUID, userLoc domain.GeoPoint, weights domain.RankingWeights, strategy ScoringStrategy) ([]domain.FeedItem, error) {
	_, span := observability.StartSpan(ctx, "ranking.ScoreVenues")
	defer span.End()

	items := make([]domain.FeedItem, 0, len(venues))

	for _, venue := range venues {
		if strategy != nil {
			item, err := strategy(ctx, venue, userID, userLoc, weights)
			if err != nil {
				observability.LogError("failed to score venue", err,
					zap.String("venue_id", venue.ID.String()))
				continue
			}
			items = append(items, item)
		} else {
			item, err := e.DefaultScoring(ctx, venue, userID, userLoc, weights)
			if err != nil {
				observability.LogError("failed to score venue", err,
					zap.String("venue_id", venue.ID.String()))
				continue
			}
			items = append(items, item)
		}
	}

	// Sort by score descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	observability.RankingScoreComputed.WithLabelValues("default").Add(float64(len(items)))

	return items, nil
}

func (e *Engine) DefaultScoring(ctx context.Context, venue domain.Venue, userID uuid.UUID, userLoc domain.GeoPoint, weights domain.RankingWeights) (domain.FeedItem, error) {
	_, span := observability.StartSpan(ctx, "ranking.DefaultScoring")
	defer span.End()

	// Popularity is capped so one blockbuster venue cannot dominate every other
	// signal; the 10k ceiling is a product assumption that should be calibrated
	// from real traffic before calling this production-ready.
	popularityScore := math.Min(float64(venue.Popularity)/10000.0, 1.0)

	// Exponential distance decay rewards nearby venues strongly while still
	// allowing exceptional farther venues to appear when other signals justify it.
	var proximityScore float64
	if userLoc.Latitude != 0 || userLoc.Longitude != 0 {
		venueLoc := domain.GeoPoint{Latitude: venue.Latitude, Longitude: venue.Longitude}
		distance := userLoc.DistanceTo(venueLoc)

		// Exponential decay: score = exp(-distance/5km)
		proximityScore = math.Exp(-distance / 5.0)

		// Penalize if too far (> 20km)
		if distance > 20.0 {
			proximityScore *= 0.1
		}
	} else {
		// No location provided, use average proximity
		proximityScore = 0.5
	}

	// Personalization score
	personalizationScore := e.calculatePersonalizationScore(ctx, userID, venue)

	// Rating score: normalize 0-5 to 0-1
	ratingScore := venue.Rating / 5.0

	// Price match score (if user has preference)
	priceMatchScore := 0.5 // neutral default

	// Trending boost is deliberately small so real-time spikes can break ties
	// without overwhelming durable quality and personalization signals.
	boost := 0.0
	if e.redis != nil {
		boost, _ = e.redis.GetVenuePopularityBoost(ctx, venue.ID.String())
	}

	// Composite score
	totalScore := weights.Popularity*popularityScore +
		weights.Proximity*proximityScore +
		weights.Personalization*personalizationScore +
		weights.Rating*ratingScore +
		weights.PriceMatch*priceMatchScore +
		boost*0.1 // small boost from real-time trends

	return domain.FeedItem{
		Venue:                venue,
		Score:                totalScore,
		PopularityScore:      popularityScore,
		ProximityScore:       proximityScore,
		PersonalizationScore: personalizationScore,
		Reason:               e.generateReason(popularityScore, proximityScore, personalizationScore),
	}, nil
}

func (e *Engine) calculatePersonalizationScore(ctx context.Context, userID uuid.UUID, venue domain.Venue) float64 {
	_, span := observability.StartSpan(ctx, "ranking.calculatePersonalizationScore")
	defer span.End()

	score := 0.0
	count := 0

	if e.redis == nil {
		return 0.3
	}

	// Check cuisine preferences
	cuisinePrefs, err := e.redis.GetCuisinePreferences(ctx, userID.String())
	if err == nil && len(cuisinePrefs) > 0 {
		if prefScore, ok := cuisinePrefs[venue.CuisineType]; ok {
			score += math.Min(prefScore/100.0, 1.0) // Normalize
			count++
		}
	}

	// Check past interactions with this venue
	interactionScores, err := e.redis.GetInteractionScores(ctx, userID.String(), "click")
	if err == nil {
		if clickScore, ok := interactionScores[venue.ID.String()]; ok {
			score += math.Min(clickScore/50.0, 1.0)
			count++
		}
	}

	orderScores, err := e.redis.GetInteractionScores(ctx, userID.String(), "order")
	if err == nil {
		if orderScore, ok := orderScores[venue.ID.String()]; ok {
			score += math.Min(orderScore/10.0, 1.0) // Orders weighted higher
			count++
		}
	}

	// Impression fatigue prevents the feed from repeatedly spending top slots on
	// a venue the user has already ignored many times.
	impressions, err := e.redis.GetImpressions(ctx, userID.String())
	if err == nil {
		if impressionCount, ok := impressions[venue.ID.String()]; ok {
			// Penalize venues seen many times (fatigue)
			fatigueFactor := math.Exp(-float64(impressionCount) / 20.0)
			score *= fatigueFactor
		}
	}

	if count == 0 {
		return 0.3 // Default score for unknown preferences
	}

	return score / float64(count)
}

func (e *Engine) generateReason(popularity, proximity, personalization float64) string {
	reasons := []string{}

	if popularity > 0.7 {
		reasons = append(reasons, "popular")
	}
	if proximity > 0.7 {
		reasons = append(reasons, "nearby")
	}
	if personalization > 0.6 {
		reasons = append(reasons, "recommended for you")
	}

	if len(reasons) == 0 {
		return "discover"
	}

	result := reasons[0]
	for i := 1; i < len(reasons); i++ {
		result += ", " + reasons[i]
	}
	return result
}

func (e *Engine) GetWeightsForVariant(variant string) domain.RankingWeights {
	switch variant {
	case "popularity-heavy":
		return domain.RankingWeights{
			Popularity:      0.55,
			Proximity:       0.25,
			Personalization: 0.10,
			Rating:          0.10,
			PriceMatch:      0.00,
		}
	case "personalization-heavy":
		return domain.RankingWeights{
			Popularity:      0.15,
			Proximity:       0.20,
			Personalization: 0.50,
			Rating:          0.10,
			PriceMatch:      0.05,
		}
	case "balanced":
		return domain.RankingWeights{
			Popularity:      0.30,
			Proximity:       0.30,
			Personalization: 0.25,
			Rating:          0.10,
			PriceMatch:      0.05,
		}
	case "rating-focused":
		return domain.RankingWeights{
			Popularity:      0.20,
			Proximity:       0.20,
			Personalization: 0.20,
			Rating:          0.35,
			PriceMatch:      0.05,
		}
	default:
		return domain.RankingWeights{
			Popularity:      0.35,
			Proximity:       0.30,
			Personalization: 0.25,
			Rating:          0.10,
			PriceMatch:      0.00,
		}
	}
}

// AdvancedScoring allows ML-inspired scoring with feature vectors
func (e *Engine) AdvancedScoring(ctx context.Context, venue domain.Venue, userID uuid.UUID, userLoc domain.GeoPoint, weights domain.RankingWeights) (domain.FeedItem, error) {
	_, span := observability.StartSpan(ctx, "ranking.AdvancedScoring")
	defer span.End()

	// Base scores
	popularityScore := math.Min(float64(venue.Popularity)/10000.0, 1.0)

	var proximityScore float64
	if userLoc.Latitude != 0 || userLoc.Longitude != 0 {
		venueLoc := domain.GeoPoint{Latitude: venue.Latitude, Longitude: venue.Longitude}
		distance := userLoc.DistanceTo(venueLoc)
		proximityScore = math.Exp(-distance / 5.0)
		if distance > 20.0 {
			proximityScore *= 0.1
		}
	} else {
		proximityScore = 0.5
	}

	personalizationScore := e.calculatePersonalizationScore(ctx, userID, venue)
	ratingScore := venue.Rating / 5.0

	// Advanced features:
	// 1. Time-based boost (certain venues popular at certain times)
	timeBoost := e.getTimeBasedBoost(venue)

	// 2. Diversity penalty (penalize if user already saw similar)
	diversityPenalty := e.getDiversityPenalty(ctx, userID, venue)

	// 3. Trending velocity (recent popularity change)
	trendingBoost := 0.0
	if e.redis != nil {
		trendingBoost, _ = e.redis.GetVenuePopularityBoost(ctx, venue.ID.String())
	}

	// Weighted combination with advanced features
	totalScore := weights.Popularity*popularityScore*(1+timeBoost) +
		weights.Proximity*proximityScore +
		weights.Personalization*personalizationScore*(1-diversityPenalty) +
		weights.Rating*ratingScore +
		weights.PriceMatch*0.5 +
		trendingBoost*0.15

	return domain.FeedItem{
		Venue:                venue,
		Score:                totalScore,
		PopularityScore:      popularityScore,
		ProximityScore:       proximityScore,
		PersonalizationScore: personalizationScore,
		Reason:               e.generateAdvancedReason(popularityScore, proximityScore, personalizationScore, timeBoost, trendingBoost),
	}, nil
}

func (e *Engine) getTimeBasedBoost(venue domain.Venue) float64 {
	hour := time.Now().Hour()

	// Example: boost breakfast places in morning, dinner in evening
	switch venue.CuisineType {
	case "breakfast", "cafe", "bakery":
		if hour >= 7 && hour <= 11 {
			return 0.3
		}
	case "lunch", "sandwich", "salad":
		if hour >= 11 && hour <= 14 {
			return 0.3
		}
	case "dinner", "fine_dining", "steakhouse":
		if hour >= 18 && hour <= 22 {
			return 0.3
		}
	case "nightlife", "bar", "pub":
		if hour >= 20 || hour <= 2 {
			return 0.4
		}
	}

	return 0.0
}

func (e *Engine) getDiversityPenalty(ctx context.Context, userID uuid.UUID, venue domain.Venue) float64 {
	if e.redis == nil {
		return 0.0
	}

	// Get recent impressions to avoid showing too many similar venues
	recentClicks, err := e.redis.GetRecentClicks(ctx, userID.String(), 5)
	if err != nil || len(recentClicks) == 0 {
		return 0.0
	}

	// If user recently clicked same cuisine type, apply small penalty
	// In a real system, would compare venue similarity more sophisticatedly
	for _, clickedVenueID := range recentClicks {
		if clickedVenueID == venue.ID.String() {
			return 0.3 // Penalize showing same venue again
		}
	}

	return 0.0
}

func (e *Engine) generateAdvancedReason(popularity, proximity, personalization, timeBoost, trending float64) string {
	if timeBoost > 0.2 {
		if proximity > 0.7 {
			return fmt.Sprintf("perfect for now, nearby (%.0fm)", proximity*1000)
		}
		return "trending right now"
	}
	if trending > 0.5 {
		return "hot right now"
	}
	return e.generateReason(popularity, proximity, personalization)
}

// ReorderForDiversity is a post-rank guardrail: it sacrifices a little score
// purity to avoid a top page that feels repetitive to the user.
func (e *Engine) ReorderForDiversity(items []domain.FeedItem, maxSameCuisine int) []domain.FeedItem {
	if len(items) <= 1 || maxSameCuisine <= 0 {
		return items
	}

	result := make([]domain.FeedItem, 0, len(items))
	remaining := make([]domain.FeedItem, len(items))
	copy(remaining, items)

	cuisineCount := make(map[string]int)

	for len(remaining) > 0 {
		found := false
		for i, item := range remaining {
			cuisine := item.Venue.CuisineType
			if cuisineCount[cuisine] < maxSameCuisine {
				result = append(result, item)
				cuisineCount[cuisine]++
				// Remove from remaining
				remaining = append(remaining[:i], remaining[i+1:]...)
				found = true
				break
			}
		}

		// If no diverse option found, just take next best
		if !found && len(remaining) > 0 {
			result = append(result, remaining[0])
			remaining = remaining[1:]
		}
	}

	return result
}
