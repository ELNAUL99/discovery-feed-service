package service

import (
	"context"
	"fmt"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/experiment"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/ranking"
	"github.com/discovery-feed-service/internal/repository/postgres"
	"github.com/discovery-feed-service/internal/repository/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type FeedService struct {
	postgres           *postgres.Repository
	redis              *redis.Repository
	rankingEngine      *ranking.Engine
	experimentSvc      *experiment.Service
	personalizationSvc *PersonalizationService
}

func NewFeedService(pg *postgres.Repository, rd *redis.Repository, engine *ranking.Engine, expSvc *experiment.Service, persSvc *PersonalizationService) *FeedService {
	return &FeedService{
		postgres:           pg,
		redis:              rd,
		rankingEngine:      engine,
		experimentSvc:      expSvc,
		personalizationSvc: persSvc,
	}
}

func (s *FeedService) GetFeed(ctx context.Context, req domain.FeedRequest) (*domain.FeedResponse, error) {
	requestID := uuid.New().String()
	start := time.Now()

	_, span := observability.StartSpan(ctx, "feed.GetFeed")
	defer span.End()

	// Parse userID
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}

	// Resolve the variant before cache lookup so each experiment arm has an
	// isolated cached feed; otherwise the first cached response would mask the
	// ranking differences the experiment is supposed to measure.
	variant := "control"
	var activeWeights domain.RankingWeights

	if experiments, err := s.experimentSvc.GetActiveExperiments(ctx); err == nil {
		for _, exp := range experiments {
			if exp.Name == "feed_ranking_v1" {
				v, err := s.experimentSvc.GetUserVariant(ctx, userID, exp.ID)
				if err == nil && v != nil {
					variant = v.Name
					activeWeights = s.experimentSvc.GetVariantConfig(v)
				}
				break
			}
		}
	}

	// Check cache
	if cached, err := s.redis.GetFeedCache(ctx, req.UserID, variant); err == nil && cached != nil {
		observability.LogInfo("feed cache hit",
			zap.String("user_id", req.UserID),
			zap.String("variant", variant),
		)
		return &domain.FeedResponse{
			Items:       cached,
			Total:       len(cached),
			Variant:     variant,
			RequestID:   requestID,
			GeneratedAt: time.Now(),
		}, nil
	}

	// Build filters
	filters := map[string]interface{}{
		"limit":  req.Limit,
		"offset": req.Offset,
	}
	if req.Cuisine != "" {
		filters["cuisine"] = req.Cuisine
	}
	if req.PriceMax > 0 {
		filters["price_max"] = req.PriceMax
	}

	// Fetch venues from DB
	venues, err := s.postgres.GetVenues(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get venues: %w", err)
	}

	// Get user location
	userLoc := domain.GeoPoint{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	}

	// Score venues
	var items []domain.FeedItem
	if variant == "advanced-ml" {
		items, err = s.rankingEngine.ScoreVenues(ctx, venues, userID, userLoc, activeWeights, s.rankingEngine.AdvancedScoring)
	} else {
		// Experiments may exist without an explicit persisted config yet, so the
		// named variant table remains the deterministic fallback for scoring.
		if activeWeights.Popularity == 0 && activeWeights.Proximity == 0 {
			activeWeights = s.rankingEngine.GetWeightsForVariant(variant)
		}
		items, err = s.rankingEngine.ScoreVenues(ctx, venues, userID, userLoc, activeWeights, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to score venues: %w", err)
	}

	// Apply diversity re-ranking for top results
	if len(items) > 0 {
		items = s.rankingEngine.ReorderForDiversity(items, 2)
	}

	// Set variant on items
	for i := range items {
		items[i].Variant = variant
	}

	// Cache result
	ttl := 2 * time.Minute
	if variant == "control" {
		// Control traffic is expected to be more stable, while active variants
		// need shorter TTLs so ranking changes and metric feedback show up sooner.
		ttl = 5 * time.Minute
	}
	_ = s.redis.SetFeedCache(ctx, req.UserID, variant, items, ttl)

	// Record only above-the-fold impressions asynchronously because they are the
	// venues most likely to have actually influenced the user's next action.
	go func() {
		bgCtx := context.Background()
		for _, item := range items[:min(10, len(items))] {
			_ = s.redis.RecordImpression(bgCtx, req.UserID, item.Venue.ID.String())
		}
	}()

	observability.LogInfo("feed generated",
		zap.String("user_id", req.UserID),
		zap.String("variant", variant),
		zap.Int("venue_count", len(venues)),
		zap.Int("result_count", len(items)),
		zap.Duration("duration", time.Since(start)),
		zap.String("request_id", requestID),
	)

	return &domain.FeedResponse{
		Items:       items,
		Total:       len(items),
		Variant:     variant,
		RequestID:   requestID,
		GeneratedAt: time.Now(),
	}, nil
}

func (s *FeedService) RecordClick(ctx context.Context, userID, venueID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	vid, err := uuid.Parse(venueID)
	if err != nil {
		return err
	}

	// Get venue info for cuisine tracking
	venue, err := s.postgres.GetVenueByID(ctx, vid)
	if err != nil {
		venue = nil
	}

	metadata := map[string]interface{}{}
	if venue != nil {
		metadata["cuisine_type"] = venue.CuisineType
		metadata["venue_name"] = venue.Name
	}

	if err := s.personalizationSvc.TrackInteraction(ctx, uid, vid, "click", 1.0, metadata); err != nil {
		observability.LogError("failed to track click", err)
	}

	// Record experiment conversion if user is in experiment
	go func() {
		bgCtx := context.Background()
		if experiments, err := s.experimentSvc.GetActiveExperiments(bgCtx); err == nil {
			for _, exp := range experiments {
				if exp.Name == "feed_ranking_v1" {
					variant, _ := s.experimentSvc.GetUserVariant(bgCtx, uid, exp.ID)
					if variant != nil {
						_ = s.experimentSvc.RecordMetric(bgCtx, exp.ID, variant.ID, "click", 1.0)
					}
				}
			}
		}
	}()

	return nil
}

func (s *FeedService) RecordOrder(ctx context.Context, userID, venueID string, orderValue float64) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	vid, err := uuid.Parse(venueID)
	if err != nil {
		return err
	}

	venue, err := s.postgres.GetVenueByID(ctx, vid)
	metadata := map[string]interface{}{}
	if venue != nil {
		metadata["cuisine_type"] = venue.CuisineType
		metadata["order_value"] = orderValue
	}

	if err := s.personalizationSvc.TrackInteraction(ctx, uid, vid, "order", orderValue, metadata); err != nil {
		return err
	}

	// Update venue popularity in Redis
	_ = s.redis.UpdateVenuePopularity(ctx, vid.String(), orderValue)

	// Record experiment conversion
	go func() {
		bgCtx := context.Background()
		if experiments, err := s.experimentSvc.GetActiveExperiments(bgCtx); err == nil {
			for _, exp := range experiments {
				if exp.Name == "feed_ranking_v1" {
					variant, _ := s.experimentSvc.GetUserVariant(bgCtx, uid, exp.ID)
					if variant != nil {
						_ = s.experimentSvc.RecordMetric(bgCtx, exp.ID, variant.ID, "conversion", orderValue)
					}
				}
			}
		}
	}()

	return nil
}

func (s *FeedService) GetUserFeedAnalytics(ctx context.Context, userID string) (map[string]interface{}, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	profile, err := s.personalizationSvc.GetUserProfile(ctx, uid)
	if err != nil {
		return nil, err
	}

	impressions, _ := s.redis.GetImpressions(ctx, userID)
	clicks, _ := s.redis.GetRecentClicks(ctx, userID, 100)

	// Calculate CTR
	ctr := 0.0
	if len(impressions) > 0 {
		ctr = float64(len(clicks)) / float64(len(impressions))
	}

	return map[string]interface{}{
		"user_id":           userID,
		"top_cuisines":      profile.CuisineScores,
		"recent_clicks":     len(clicks),
		"total_impressions": len(impressions),
		"ctr":               ctr,
		"price_preference":  profile.Preferences.PricePreference,
		"max_distance":      profile.Preferences.MaxDistance,
	}, nil
}
