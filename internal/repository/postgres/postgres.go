package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/discovery-feed-service/internal/domain"
	"github.com/discovery-feed-service/internal/observability"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(dsn string) (*Repository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) DB() *sql.DB {
	return r.db
}

func (r *Repository) GetVenues(ctx context.Context, filters map[string]interface{}) ([]domain.Venue, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_venues").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, name, description, cuisine_type, latitude, longitude, rating, popularity, price_level, is_active, created_at, updated_at 
			  FROM venues WHERE is_active = true`

	args := []interface{}{}
	argCount := 1

	if cuisine, ok := filters["cuisine"]; ok && cuisine != "" {
		query += fmt.Sprintf(" AND cuisine_type = $%d", argCount)
		args = append(args, cuisine)
		argCount++
	}

	if priceMax, ok := filters["price_max"]; ok && priceMax.(int) > 0 {
		query += fmt.Sprintf(" AND price_level <= $%d", argCount)
		args = append(args, priceMax)
		argCount++
	}

	query += " ORDER BY popularity DESC"

	if limit, ok := filters["limit"]; ok {
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, limit)
		argCount++
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query venues: %w", err)
	}
	defer rows.Close()

	var venues []domain.Venue
	for rows.Next() {
		var v domain.Venue
		err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.CuisineType, &v.Latitude, &v.Longitude,
			&v.Rating, &v.Popularity, &v.PriceLevel, &v.IsActive, &v.CreatedAt, &v.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan venue: %w", err)
		}
		venues = append(venues, v)
	}

	return venues, nil
}

func (r *Repository) GetVenueByID(ctx context.Context, id uuid.UUID) (*domain.Venue, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_venue_by_id").Observe(time.Since(start).Seconds())
	}()

	var v domain.Venue
	query := `SELECT id, name, description, cuisine_type, latitude, longitude, rating, popularity, price_level, is_active, created_at, updated_at 
			  FROM venues WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&v.ID, &v.Name, &v.Description, &v.CuisineType, &v.Latitude, &v.Longitude,
		&v.Rating, &v.Popularity, &v.PriceLevel, &v.IsActive, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get venue: %w", err)
	}

	return &v, nil
}

func (r *Repository) RecordInteraction(ctx context.Context, interaction *domain.UserInteraction) error {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("record_interaction").Observe(time.Since(start).Seconds())
	}()

	query := `INSERT INTO user_interactions (id, user_id, venue_id, type, value, metadata, created_at) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := r.db.ExecContext(ctx, query,
		interaction.ID, interaction.UserID, interaction.VenueID,
		interaction.Type, interaction.Value, interaction.Metadata, interaction.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record interaction: %w", err)
	}

	return nil
}

func (r *Repository) GetUserInteractions(ctx context.Context, userID uuid.UUID, interactionType string, limit int) ([]domain.UserInteraction, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_user_interactions").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, user_id, venue_id, type, value, metadata, created_at 
			  FROM user_interactions WHERE user_id = $1`
	args := []interface{}{userID}
	argCount := 2

	if interactionType != "" {
		query += fmt.Sprintf(" AND type = $%d", argCount)
		args = append(args, interactionType)
		argCount++
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get user interactions: %w", err)
	}
	defer rows.Close()

	var interactions []domain.UserInteraction
	for rows.Next() {
		var i domain.UserInteraction
		err := rows.Scan(&i.ID, &i.UserID, &i.VenueID, &i.Type, &i.Value, &i.Metadata, &i.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan interaction: %w", err)
		}
		interactions = append(interactions, i)
	}

	return interactions, nil
}

func (r *Repository) GetUserPreferences(ctx context.Context, userID uuid.UUID) (*domain.UserPreference, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_user_preferences").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT user_id, cuisine_weights, price_preference, max_distance, updated_at 
			  FROM user_preferences WHERE user_id = $1`

	var p domain.UserPreference
	var cuisineWeightsJSON string

	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&p.UserID, &cuisineWeightsJSON, &p.PricePreference, &p.MaxDistance, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		// New users need a neutral profile immediately so feed generation can
		// proceed before enough behavior exists to infer durable preferences.
		return &domain.UserPreference{
			UserID:          userID,
			CuisineWeights:  map[string]float64{},
			PricePreference: 2,
			MaxDistance:     10.0,
			UpdatedAt:       time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user preferences: %w", err)
	}

	return &p, nil
}

func (r *Repository) UpdateUserPreferences(ctx context.Context, pref *domain.UserPreference) error {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("update_user_preferences").Observe(time.Since(start).Seconds())
	}()

	query := `INSERT INTO user_preferences (user_id, cuisine_weights, price_preference, max_distance, updated_at)
			  VALUES ($1, $2, $3, $4, $5)
			  ON CONFLICT (user_id) DO UPDATE SET
			  cuisine_weights = EXCLUDED.cuisine_weights,
			  price_preference = EXCLUDED.price_preference,
			  max_distance = EXCLUDED.max_distance,
			  updated_at = EXCLUDED.updated_at`

	cuisineWeightsJSON := "{}"
	// In real implementation, would marshal the map to JSON

	_, err := r.db.ExecContext(ctx, query,
		pref.UserID, cuisineWeightsJSON, pref.PricePreference, pref.MaxDistance, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to update preferences: %w", err)
	}

	return nil
}

func (r *Repository) GetActiveExperiments(ctx context.Context) ([]domain.Experiment, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_active_experiments").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, name, description, status, start_date, end_date, created_at 
			  FROM experiments WHERE status = 'active' AND start_date <= NOW() AND end_date >= NOW()`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get experiments: %w", err)
	}
	defer rows.Close()

	var experiments []domain.Experiment
	for rows.Next() {
		var e domain.Experiment
		err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.Status, &e.StartDate, &e.EndDate, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan experiment: %w", err)
		}
		experiments = append(experiments, e)
	}

	return experiments, nil
}

func (r *Repository) GetExperimentVariants(ctx context.Context, experimentID uuid.UUID) ([]domain.Variant, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_experiment_variants").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, experiment_id, name, config, traffic_pct, created_at 
			  FROM variants WHERE experiment_id = $1`

	rows, err := r.db.QueryContext(ctx, query, experimentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get variants: %w", err)
	}
	defer rows.Close()

	var variants []domain.Variant
	for rows.Next() {
		var v domain.Variant
		err := rows.Scan(&v.ID, &v.ExperimentID, &v.Name, &v.Config, &v.TrafficPct, &v.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan variant: %w", err)
		}
		variants = append(variants, v)
	}

	return variants, nil
}

func (r *Repository) GetUserAssignment(ctx context.Context, userID, experimentID uuid.UUID) (*domain.ExperimentAssignment, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_user_assignment").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT user_id, experiment_id, variant_id, assigned_at 
			  FROM experiment_assignments WHERE user_id = $1 AND experiment_id = $2`

	var a domain.ExperimentAssignment
	err := r.db.QueryRowContext(ctx, query, userID, experimentID).Scan(
		&a.UserID, &a.ExperimentID, &a.VariantID, &a.AssignedAt,
	)
	if err != nil {
		return nil, err
	}

	return &a, nil
}

func (r *Repository) CreateAssignment(ctx context.Context, assignment *domain.ExperimentAssignment) error {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("create_assignment").Observe(time.Since(start).Seconds())
	}()

	query := `INSERT INTO experiment_assignments (user_id, experiment_id, variant_id, assigned_at)
			  VALUES ($1, $2, $3, $4)
			  ON CONFLICT (user_id, experiment_id) DO NOTHING`

	_, err := r.db.ExecContext(ctx, query,
		assignment.UserID, assignment.ExperimentID, assignment.VariantID, assignment.AssignedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create assignment: %w", err)
	}

	return nil
}

func (r *Repository) RecordExperimentMetric(ctx context.Context, metric *domain.ExperimentMetric) error {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("record_experiment_metric").Observe(time.Since(start).Seconds())
	}()

	query := `INSERT INTO experiment_metrics (id, experiment_id, variant_id, metric_type, value, count, recorded_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := r.db.ExecContext(ctx, query,
		metric.ID, metric.ExperimentID, metric.VariantID, metric.MetricType,
		metric.Value, metric.Count, metric.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record metric: %w", err)
	}

	return nil
}

func (r *Repository) GetRankingConfig(ctx context.Context, id uuid.UUID) (*domain.RankingConfig, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_ranking_config").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, name, weights, is_default, created_at, updated_at 
			  FROM ranking_configs WHERE id = $1`

	var c domain.RankingConfig
	var weightsJSON string

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.Name, &weightsJSON, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get ranking config: %w", err)
	}

	return &c, nil
}

func (r *Repository) GetDefaultRankingConfig(ctx context.Context) (*domain.RankingConfig, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_default_ranking_config").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, name, weights, is_default, created_at, updated_at 
			  FROM ranking_configs WHERE is_default = true LIMIT 1`

	var c domain.RankingConfig
	var weightsJSON string

	err := r.db.QueryRowContext(ctx, query).Scan(
		&c.ID, &c.Name, &weightsJSON, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		// Return default config
		return &domain.RankingConfig{
			ID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Name: "default",
			Weights: domain.RankingWeights{
				Popularity:      0.35,
				Proximity:       0.30,
				Personalization: 0.25,
				Rating:          0.10,
			},
			IsDefault: true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default config: %w", err)
	}

	return &c, nil
}

func (r *Repository) GetRankingConfigs(ctx context.Context) ([]domain.RankingConfig, error) {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("get_ranking_configs").Observe(time.Since(start).Seconds())
	}()

	query := `SELECT id, name, weights, is_default, created_at, updated_at FROM ranking_configs`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get ranking configs: %w", err)
	}
	defer rows.Close()

	var configs []domain.RankingConfig
	for rows.Next() {
		var c domain.RankingConfig
		var weightsJSON string
		err := rows.Scan(&c.ID, &c.Name, &weightsJSON, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan config: %w", err)
		}
		configs = append(configs, c)
	}

	return configs, nil
}

func (r *Repository) SaveRankingConfig(ctx context.Context, config *domain.RankingConfig) error {
	start := time.Now()
	defer func() {
		observability.DBQueryDuration.WithLabelValues("save_ranking_config").Observe(time.Since(start).Seconds())
	}()

	query := `INSERT INTO ranking_configs (id, name, weights, is_default, created_at, updated_at)
			  VALUES ($1, $2, $3, $4, $5, $6)
			  ON CONFLICT (id) DO UPDATE SET
			  name = EXCLUDED.name,
			  weights = EXCLUDED.weights,
			  is_default = EXCLUDED.is_default,
			  updated_at = EXCLUDED.updated_at`

	weightsJSON := "{}"
	_, err := r.db.ExecContext(ctx, query,
		config.ID, config.Name, weightsJSON, config.IsDefault, config.CreatedAt, config.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save ranking config: %w", err)
	}

	return nil
}
