package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

type Venue struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CuisineType string    `json:"cuisine_type" db:"cuisine_type"`
	Latitude    float64   `json:"latitude" db:"latitude"`
	Longitude   float64   `json:"longitude" db:"longitude"`
	Rating      float64   `json:"rating" db:"rating"`
	Popularity  int       `json:"popularity" db:"popularity"`
	PriceLevel  int       `json:"price_level" db:"price_level"`
	IsActive    bool      `json:"is_active" db:"is_active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type FeedItem struct {
	Venue               Venue   `json:"venue"`
	Score               float64 `json:"score"`
	PopularityScore     float64 `json:"popularity_score"`
	ProximityScore      float64 `json:"proximity_score"`
	PersonalizationScore float64 `json:"personalization_score"`
	Variant             string  `json:"variant,omitempty"`
	Reason              string  `json:"reason,omitempty"`
}

type FeedRequest struct {
	UserID    string  `form:"user_id" binding:"required"`
	Latitude  float64 `form:"latitude"`
	Longitude float64 `form:"longitude"`
	Limit     int     `form:"limit,default=20"`
	Offset    int     `form:"offset,default=0"`
	Cuisine   string  `form:"cuisine"`
	PriceMax  int     `form:"price_max"`
}

type FeedResponse struct {
	Items      []FeedItem `json:"items"`
	Total      int        `json:"total"`
	Variant    string     `json:"variant,omitempty"`
	RequestID  string     `json:"request_id"`
	GeneratedAt time.Time `json:"generated_at"`
}

type UserInteraction struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	VenueID   uuid.UUID `json:"venue_id" db:"venue_id"`
	Type      string    `json:"type" db:"type"` // click, impression, order, favorite
	Value     float64   `json:"value" db:"value"`
	Metadata  string    `json:"metadata" db:"metadata"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type UserPreference struct {
	UserID        uuid.UUID `json:"user_id" db:"user_id"`
	CuisineWeights map[string]float64 `json:"cuisine_weights"`
	PricePreference int    `json:"price_preference"`
	MaxDistance     float64 `json:"max_distance"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

type Experiment struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"` // active, paused, completed
	StartDate   time.Time `json:"start_date" db:"start_date"`
	EndDate     time.Time `json:"end_date" db:"end_date"`
	Variants    []Variant `json:"variants"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type Variant struct {
	ID           uuid.UUID `json:"id" db:"id"`
	ExperimentID uuid.UUID `json:"experiment_id" db:"experiment_id"`
	Name         string    `json:"name" db:"name"`
	Config       string    `json:"config" db:"config"` // JSON ranking config
	TrafficPct   float64   `json:"traffic_pct" db:"traffic_pct"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

type ExperimentAssignment struct {
	UserID       uuid.UUID `json:"user_id" db:"user_id"`
	ExperimentID uuid.UUID `json:"experiment_id" db:"experiment_id"`
	VariantID    uuid.UUID `json:"variant_id" db:"variant_id"`
	AssignedAt   time.Time `json:"assigned_at" db:"assigned_at"`
}

type ExperimentMetric struct {
	ID           uuid.UUID `json:"id" db:"id"`
	ExperimentID uuid.UUID `json:"experiment_id" db:"experiment_id"`
	VariantID    uuid.UUID `json:"variant_id" db:"variant_id"`
	MetricType   string    `json:"metric_type" db:"metric_type"` // ctr, conversion, engagement
	Value        float64   `json:"value" db:"value"`
	Count        int       `json:"count" db:"count"`
	RecordedAt   time.Time `json:"recorded_at" db:"recorded_at"`
}

type RankingConfig struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Weights   RankingWeights `json:"weights"`
	IsDefault bool      `json:"is_default" db:"is_default"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type RankingWeights struct {
	Popularity      float64 `json:"popularity"`
	Proximity       float64 `json:"proximity"`
	Personalization float64 `json:"personalization"`
	Rating          float64 `json:"rating"`
	Recency         float64 `json:"recency"`
	PriceMatch      float64 `json:"price_match"`
}

type AIRecommendation struct {
	ID          uuid.UUID `json:"id"`
	Type        string    `json:"type"` // weight_adjustment, new_feature, config_change
	Description string    `json:"description"`
	Diff        string    `json:"diff"`
	Confidence  float64   `json:"confidence"`
	Impact      float64   `json:"impact"`
	CreatedAt   time.Time `json:"created_at"`
}

type SimulationResult struct {
	ConfigID    uuid.UUID `json:"config_id"`
	ConfigName  string    `json:"config_name"`
	AvgCTR      float64   `json:"avg_ctr"`
	AvgConversion float64 `json:"avg_conversion"`
	UserSatisfaction float64 `json:"user_satisfaction"`
	TotalScore  float64   `json:"total_score"`
}

type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (g GeoPoint) DistanceTo(other GeoPoint) float64 {
	const R = 6371 // Earth radius in km
	lat1Rad := g.Latitude * math.Pi / 180
	lat2Rad := other.Latitude * math.Pi / 180
	deltaLat := (other.Latitude - g.Latitude) * math.Pi / 180
	deltaLon := (other.Longitude - g.Longitude) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
