package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type Repository struct {
	client *redis.Client
}

func NewRepository(addr, password string, db, poolSize int) *Repository {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		PoolSize: poolSize,
	})

	return &Repository{client: client}
}

func (r *Repository) Close() error {
	return r.client.Close()
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *Repository) GetFeedCache(ctx context.Context, userID string, variant string) ([]domain.FeedItem, error) {
	key := fmt.Sprintf("feed:%s:%s", userID, variant)
	
	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		observability.CacheMisses.WithLabelValues("feed").Inc()
		return nil, nil
	}
	if err != nil {
		observability.CacheMisses.WithLabelValues("feed").Inc()
		return nil, err
	}

	observability.CacheHits.WithLabelValues("feed").Inc()

	var items []domain.FeedItem
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feed cache: %w", err)
	}

	return items, nil
}

func (r *Repository) SetFeedCache(ctx context.Context, userID string, variant string, items []domain.FeedItem, ttl time.Duration) error {
	key := fmt.Sprintf("feed:%s:%s", userID, variant)
	
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("failed to marshal feed cache: %w", err)
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *Repository) GetUserPreferencesCache(ctx context.Context, userID uuid.UUID) (*domain.UserPreference, error) {
	key := fmt.Sprintf("prefs:%s", userID.String())
	
	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		observability.CacheMisses.WithLabelValues("preferences").Inc()
		return nil, nil
	}
	if err != nil {
		observability.CacheMisses.WithLabelValues("preferences").Inc()
		return nil, err
	}

	observability.CacheHits.WithLabelValues("preferences").Inc()

	var pref domain.UserPreference
	if err := json.Unmarshal([]byte(data), &pref); err != nil {
		return nil, fmt.Errorf("failed to unmarshal preferences: %w", err)
	}

	return &pref, nil
}

func (r *Repository) SetUserPreferencesCache(ctx context.Context, pref *domain.UserPreference, ttl time.Duration) error {
	key := fmt.Sprintf("prefs:%s", pref.UserID.String())
	
	data, err := json.Marshal(pref)
	if err != nil {
		return fmt.Errorf("failed to marshal preferences: %w", err)
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *Repository) IncrementInteractionScore(ctx context.Context, userID, venueID string, interactionType string, value float64) error {
	key := fmt.Sprintf("interactions:%s:%s", userID, interactionType)
	field := venueID
	
	pipe := r.client.Pipeline()
	pipe.HIncrByFloat(ctx, key, field, value)
	pipe.Expire(ctx, key, 30*24*time.Hour)
	
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Repository) GetInteractionScores(ctx context.Context, userID string, interactionType string) (map[string]float64, error) {
	key := fmt.Sprintf("interactions:%s:%s", userID, interactionType)
	
	scores, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]float64, len(scores))
	for k, v := range scores {
		var score float64
		fmt.Sscanf(v, "%f", &score)
		result[k] = score
	}

	return result, nil
}

func (r *Repository) RecordClick(ctx context.Context, userID, venueID string) error {
	now := time.Now().Unix()
	
	// Add to sorted set for time-series data
	key := fmt.Sprintf("clicks:%s", userID)
	return r.client.ZAdd(ctx, key, &redis.Z{
		Score:  float64(now),
		Member: venueID,
	}).Err()
}

func (r *Repository) GetRecentClicks(ctx context.Context, userID string, limit int64) ([]string, error) {
	key := fmt.Sprintf("clicks:%s", userID)
	
	results, err := r.client.ZRevRange(ctx, key, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}

	return results, nil
}

func (r *Repository) UpdateCuisinePreference(ctx context.Context, userID string, cuisine string, score float64) error {
	key := fmt.Sprintf("cuisine_prefs:%s", userID)
	
	pipe := r.client.Pipeline()
	pipe.HIncrByFloat(ctx, key, cuisine, score)
	pipe.HSet(ctx, key, "_updated", time.Now().Unix())
	pipe.Expire(ctx, key, 90*24*time.Hour)
	
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Repository) GetCuisinePreferences(ctx context.Context, userID string) (map[string]float64, error) {
	key := fmt.Sprintf("cuisine_prefs:%s", userID)
	
	prefs, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]float64)
	for k, v := range prefs {
		if k == "_updated" {
			continue
		}
		var score float64
		fmt.Sscanf(v, "%f", &score)
		result[k] = score
	}

	return result, nil
}

func (r *Repository) GetExperimentVariantCache(ctx context.Context, userID, experimentID string) (string, error) {
	key := fmt.Sprintf("exp:%s:%s", experimentID, userID)
	
	variant, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		observability.CacheMisses.WithLabelValues("experiment").Inc()
		return "", nil
	}
	if err != nil {
		observability.CacheMisses.WithLabelValues("experiment").Inc()
		return "", err
	}

	observability.CacheHits.WithLabelValues("experiment").Inc()
	return variant, nil
}

func (r *Repository) SetExperimentVariantCache(ctx context.Context, userID, experimentID, variant string, ttl time.Duration) error {
	key := fmt.Sprintf("exp:%s:%s", experimentID, userID)
	return r.client.Set(ctx, key, variant, ttl).Err()
}

func (r *Repository) RecordImpression(ctx context.Context, userID, venueID string) error {
	key := fmt.Sprintf("impressions:%s", userID)
	now := float64(time.Now().Unix())
	
	pipe := r.client.Pipeline()
	pipe.ZAdd(ctx, key, &redis.Z{Score: now, Member: venueID})
	pipe.ZRemRangeByRank(ctx, key, 0, -1001) // Keep last 1000
	pipe.Expire(ctx, key, 30*24*time.Hour)
	
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Repository) GetImpressions(ctx context.Context, userID string) (map[string]int, error) {
	key := fmt.Sprintf("impressions:%s", userID)
	
	results, err := r.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, venueID := range results {
		counts[venueID]++
	}

	return counts, nil
}

func (r *Repository) GetVenuePopularityBoost(ctx context.Context, venueID string) (float64, error) {
	key := fmt.Sprintf("venue_boost:%s", venueID)
	
	boost, err := r.client.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	return boost, nil
}

func (r *Repository) SetVenuePopularityBoost(ctx context.Context, venueID string, boost float64, ttl time.Duration) error {
	key := fmt.Sprintf("venue_boost:%s", venueID)
	return r.client.Set(ctx, key, boost, ttl).Err()
}

func (r *Repository) GetRankingConfigCache(ctx context.Context, configID string) (*domain.RankingConfig, error) {
	key := fmt.Sprintf("ranking_config:%s", configID)
	
	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		observability.CacheMisses.WithLabelValues("ranking_config").Inc()
		return nil, nil
	}
	if err != nil {
		observability.CacheMisses.WithLabelValues("ranking_config").Inc()
		return nil, err
	}

	observability.CacheHits.WithLabelValues("ranking_config").Inc()

	var config domain.RankingConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ranking config: %w", err)
	}

	return &config, nil
}

func (r *Repository) SetRankingConfigCache(ctx context.Context, config *domain.RankingConfig, ttl time.Duration) error {
	key := fmt.Sprintf("ranking_config:%s", config.ID.String())
	
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal ranking config: %w", err)
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *Repository) GetNearbyVenues(ctx context.Context, lat, lon, radiusKm float64) ([]string, error) {
	// Use Redis GEOSEARCH if available, otherwise return empty
	// This is a simplified implementation
	key := "venue_locations"
	
	// Add venues to geospatial index first (should be done during venue creation)
	results, err := r.client.GeoRadius(ctx, key, lon, lat, &redis.GeoRadiusQuery{
		Radius: radiusKm,
		Unit:   "km",
		WithDist: true,
		Count:  50,
	}).Result()
	if err != nil {
		return nil, err
	}

	venueIDs := make([]string, 0, len(results))
	for _, loc := range results {
		venueIDs = append(venueIDs, loc.Name)
	}

	return venueIDs, nil
}

func (r *Repository) AddVenueLocation(ctx context.Context, venueID string, lat, lon float64) error {
	key := "venue_locations"
	return r.client.GeoAdd(ctx, key, &redis.GeoLocation{
		Name:      venueID,
		Longitude: lon,
		Latitude:  lat,
	}).Err()
}

func (r *Repository) GetTopPopularVenues(ctx context.Context, limit int64) ([]string, error) {
	key := "venue_popularity"
	
	results, err := r.client.ZRevRange(ctx, key, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}

	return results, nil
}

func (r *Repository) UpdateVenuePopularity(ctx context.Context, venueID string, score float64) error {
	key := "venue_popularity"
	return r.client.ZAdd(ctx, key, &redis.Z{Score: score, Member: venueID}).Err()
}

func (r *Repository) RecordEvent(ctx context.Context, eventType string, data map[string]interface{}) error {
	// Stream events to Redis Stream for lightweight event streaming
	key := fmt.Sprintf("events:%s", eventType)
	
	values := make(map[string]interface{}, len(data))
	for k, v := range data {
		values[k] = v
	}
	values["timestamp"] = time.Now().Unix()

	return r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		Values: values,
	}).Err()
}

func (r *Repository) GetRealtimeTrendingVenues(ctx context.Context, window time.Duration, limit int) ([]string, error) {
	// Get venues with recent interaction spikes
	key := "realtime_trending"
	now := float64(time.Now().Unix())
	minScore := now - window.Seconds()
	
	// Use a sliding window approach with sorted sets
	results, err := r.client.ZRevRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", minScore),
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil {
		return nil, err
	}

	venueCounts := make(map[string]int)
	for _, z := range results {
		venueCounts[z.Member.(string)]++
	}

	// Sort by count
	type pair struct {
		venueID string
		count   int
	}
	pairs := make([]pair, 0, len(venueCounts))
	for v, c := range venueCounts {
		pairs = append(pairs, pair{v, c})
	}

	// Simple bubble sort for top venues
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].count > pairs[i].count {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	result := make([]string, 0, int(math.Min(float64(len(pairs)), float64(limit))))
	for i := 0; i < len(pairs) && i < limit; i++ {
		result = append(result, pairs[i].venueID)
	}

	return result, nil
}
