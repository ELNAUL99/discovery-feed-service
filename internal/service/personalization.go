package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/discovery-feed-service/internal/repository/postgres"
	"github.com/discovery-feed-service/internal/repository/redis"
	"github.com/google/uuid"
)

type PersonalizationService struct {
	postgres *postgres.Repository
	redis    *redis.Repository
}

func NewPersonalizationService(pg *postgres.Repository, rd *redis.Repository) *PersonalizationService {
	return &PersonalizationService{
		postgres: pg,
		redis:    rd,
	}
}

// TrackInteraction records a user interaction and updates preferences
func (s *PersonalizationService) TrackInteraction(ctx context.Context, userID, venueID uuid.UUID, interactionType string, value float64, metadata map[string]interface{}) error {
	_, span := observability.StartSpan(ctx, "personalization.TrackInteraction")
	defer span.End()

	// Create interaction record
	interaction := &domain.UserInteraction{
		ID:        uuid.New(),
		UserID:    userID,
		VenueID:   venueID,
		Type:      interactionType,
		Value:     value,
		Metadata:  s.metadataToString(metadata),
		CreatedAt: time.Now(),
	}

	if err := s.postgres.RecordInteraction(ctx, interaction); err != nil {
		observability.LogError("failed to record interaction to DB", err)
	}

	// Update Redis caches for real-time personalization
	if err := s.redis.IncrementInteractionScore(ctx, userID.String(), venueID.String(), interactionType, value); err != nil {
		observability.LogError("failed to update interaction score in Redis", err)
	}

	// Update cuisine preferences if venue info available
	if metadata != nil {
		if cuisine, ok := metadata["cuisine_type"].(string); ok {
			score := s.getInteractionWeight(interactionType) * value
			if err := s.redis.UpdateCuisinePreference(ctx, userID.String(), cuisine, score); err != nil {
				observability.LogError("failed to update cuisine preference", err)
			}
		}
	}

	// Record click for recency tracking
	if interactionType == "click" {
		if err := s.redis.RecordClick(ctx, userID.String(), venueID.String()); err != nil {
			observability.LogError("failed to record click", err)
		}
	}

	// Record impression
	if interactionType == "impression" {
		if err := s.redis.RecordImpression(ctx, userID.String(), venueID.String()); err != nil {
			observability.LogError("failed to record impression", err)
		}
	}

	// Async: update preference model
	go s.updatePreferenceModel(context.Background(), userID)

	return nil
}

// GetUserPreferences returns user preferences with caching
func (s *PersonalizationService) GetUserPreferences(ctx context.Context, userID uuid.UUID) (*domain.UserPreference, error) {
	_, span := observability.StartSpan(ctx, "personalization.GetUserPreferences")
	defer span.End()

	// Try cache first
	cached, err := s.redis.GetUserPreferencesCache(ctx, userID)
	if err == nil && cached != nil {
		return cached, nil
	}

	// Get from DB
	prefs, err := s.postgres.GetUserPreferences(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get preferences: %w", err)
	}

	// Enhance with Redis real-time data
	cuisinePrefs, err := s.redis.GetCuisinePreferences(ctx, userID.String())
	if err == nil && len(cuisinePrefs) > 0 {
		if prefs.CuisineWeights == nil {
			prefs.CuisineWeights = make(map[string]float64)
		}
		for cuisine, score := range cuisinePrefs {
			prefs.CuisineWeights[cuisine] = score
		}
	}

	// Cache for future requests
	if err := s.redis.SetUserPreferencesCache(ctx, prefs, 5*time.Minute); err != nil {
		observability.LogError("failed to cache preferences", err)
	}

	return prefs, nil
}

// GetRecommendedCuisines returns top cuisines for a user
func (s *PersonalizationService) GetRecommendedCuisines(ctx context.Context, userID uuid.UUID, limit int) ([]string, error) {
	prefs, err := s.GetUserPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}

	type cuisineScore struct {
		cuisine string
		score   float64
	}

	scores := make([]cuisineScore, 0, len(prefs.CuisineWeights))
	for cuisine, score := range prefs.CuisineWeights {
		scores = append(scores, cuisineScore{cuisine, score})
	}

	// Sort by score descending
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	result := make([]string, 0, int(math.Min(float64(len(scores)), float64(limit))))
	for i := 0; i < len(scores) && i < limit; i++ {
		result = append(result, scores[i].cuisine)
	}

	return result, nil
}

// GetUserProfile returns a comprehensive user profile for personalization
func (s *PersonalizationService) GetUserProfile(ctx context.Context, userID uuid.UUID) (*UserProfile, error) {
	prefs, err := s.GetUserPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}

	recentClicks, err := s.redis.GetRecentClicks(ctx, userID.String(), 10)
	if err != nil {
		recentClicks = []string{}
	}

	cuisinePrefs, err := s.redis.GetCuisinePreferences(ctx, userID.String())
	if err != nil {
		cuisinePrefs = map[string]float64{}
	}

	interactions, err := s.postgres.GetUserInteractions(ctx, userID, "", 50)
	if err != nil {
		interactions = []domain.UserInteraction{}
	}

	return &UserProfile{
		UserID:             userID,
		Preferences:        prefs,
		RecentClicks:       recentClicks,
		CuisineScores:      cuisinePrefs,
		RecentInteractions: interactions,
	}, nil
}

// updatePreferenceModel updates the user's preference model based on recent interactions
func (s *PersonalizationService) updatePreferenceModel(ctx context.Context, userID uuid.UUID) {
	// Get recent interactions
	interactions, err := s.postgres.GetUserInteractions(ctx, userID, "", 100)
	if err != nil {
		observability.LogError("failed to get interactions for model update", err)
		return
	}

	// Aggregate preferences
	cuisineScores := make(map[string]float64)
	venueScores := make(map[string]float64)

	for _, interaction := range interactions {
		weight := s.getInteractionWeight(interaction.Type)

		// In a real system, would fetch venue info to get cuisine type
		// For now, just track venue scores
		venueScores[interaction.VenueID.String()] += weight * interaction.Value
	}

	// Update cuisine preferences from Redis data
	existingPrefs, _ := s.redis.GetCuisinePreferences(ctx, userID.String())
	for cuisine, score := range existingPrefs {
		cuisineScores[cuisine] = score
	}

	// Save updated preferences to DB
	pref := &domain.UserPreference{
		UserID:          userID,
		CuisineWeights:  cuisineScores,
		PricePreference: 2, // Default, could be inferred
		MaxDistance:     10.0,
		UpdatedAt:       time.Now(),
	}

	if err := s.postgres.UpdateUserPreferences(ctx, pref); err != nil {
		observability.LogError("failed to update preference model", err)
	}

	// Invalidate cache
	_ = s.redis.SetUserPreferencesCache(ctx, pref, 1*time.Second) // Short TTL to force refresh
}

func (s *PersonalizationService) getInteractionWeight(interactionType string) float64 {
	switch interactionType {
	case "order":
		return 5.0
	case "favorite":
		return 3.0
	case "click":
		return 1.0
	case "impression":
		return 0.1
	case "dismiss":
		return -1.0
	default:
		return 0.5
	}
}

func (s *PersonalizationService) metadataToString(metadata map[string]interface{}) string {
	if metadata == nil {
		return "{}"
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(data)
}

type UserProfile struct {
	UserID             uuid.UUID                `json:"user_id"`
	Preferences        *domain.UserPreference    `json:"preferences"`
	RecentClicks       []string                 `json:"recent_clicks"`
	CuisineScores      map[string]float64       `json:"cuisine_scores"`
	RecentInteractions []domain.UserInteraction `json:"recent_interactions"`
}
