-- Migration: Initial schema for Discovery Feed Service
-- Created: 2024-01-01

-- Venues table
CREATE TABLE IF NOT EXISTS venues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    cuisine_type VARCHAR(50) NOT NULL,
    latitude DECIMAL(10, 8) NOT NULL,
    longitude DECIMAL(11, 8) NOT NULL,
    rating DECIMAL(3, 2) DEFAULT 0.0,
    popularity INTEGER DEFAULT 0,
    price_level INTEGER DEFAULT 2,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- User interactions table
CREATE TABLE IF NOT EXISTS user_interactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    venue_id UUID NOT NULL REFERENCES venues(id),
    type VARCHAR(20) NOT NULL, -- click, impression, order, favorite, dismiss
    value DECIMAL(10, 2) DEFAULT 1.0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_user_interactions_user_id ON user_interactions(user_id);
CREATE INDEX idx_user_interactions_venue_id ON user_interactions(venue_id);
CREATE INDEX idx_user_interactions_type ON user_interactions(type);
CREATE INDEX idx_user_interactions_created_at ON user_interactions(created_at);

-- User preferences table
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY,
    cuisine_weights JSONB DEFAULT '{}',
    price_preference INTEGER DEFAULT 2,
    max_distance DECIMAL(5, 2) DEFAULT 10.0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Experiments table
CREATE TABLE IF NOT EXISTS experiments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    status VARCHAR(20) DEFAULT 'active', -- active, paused, completed
    start_date TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    end_date TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Variants table
CREATE TABLE IF NOT EXISTS variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    config JSONB DEFAULT '{}',
    traffic_pct DECIMAL(5, 4) DEFAULT 0.5,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_variants_experiment_id ON variants(experiment_id);

-- Experiment assignments table
CREATE TABLE IF NOT EXISTS experiment_assignments (
    user_id UUID NOT NULL,
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES variants(id),
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (user_id, experiment_id)
);

CREATE INDEX idx_experiment_assignments_experiment_id ON experiment_assignments(experiment_id);
CREATE INDEX idx_experiment_assignments_variant_id ON experiment_assignments(variant_id);

-- Experiment metrics table
CREATE TABLE IF NOT EXISTS experiment_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES variants(id),
    metric_type VARCHAR(50) NOT NULL, -- ctr, conversion, engagement_time
    value DECIMAL(10, 4) DEFAULT 0.0,
    count INTEGER DEFAULT 1,
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_experiment_metrics_experiment_id ON experiment_metrics(experiment_id);
CREATE INDEX idx_experiment_metrics_variant_id ON experiment_metrics(variant_id);
CREATE INDEX idx_experiment_metrics_recorded_at ON experiment_metrics(recorded_at);

-- Ranking configs table
CREATE TABLE IF NOT EXISTS ranking_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    weights JSONB DEFAULT '{}',
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_ranking_configs_is_default ON ranking_configs(is_default) WHERE is_default = true;

-- Insert default ranking config
INSERT INTO ranking_configs (id, name, weights, is_default) VALUES
    ('00000000-0000-0000-0000-000000000001', 'default', '{"popularity": 0.35, "proximity": 0.30, "personalization": 0.25, "rating": 0.10, "price_match": 0.00, "recency": 0.00}', true)
ON CONFLICT (id) DO NOTHING;

-- Insert sample venues
INSERT INTO venues (name, description, cuisine_type, latitude, longitude, rating, popularity, price_level) VALUES
    ('Burger Joint', 'Best burgers in town', 'burger', 40.7128, -74.0060, 4.5, 8500, 2),
    ('Sushi Palace', 'Fresh sushi daily', 'sushi', 40.7580, -73.9855, 4.7, 9200, 3),
    ('Pizza Heaven', 'Wood-fired pizza', 'pizza', 40.7282, -73.7949, 4.3, 7800, 2),
    ('Taco Express', 'Authentic Mexican tacos', 'mexican', 40.6892, -74.0445, 4.4, 6500, 1),
    ('Curry House', 'Indian cuisine specialists', 'indian', 40.7489, -73.9680, 4.6, 7100, 2),
    ('Pasta Place', 'Homemade Italian pasta', 'italian', 40.7614, -73.9776, 4.2, 5400, 2),
    ('Dim Sum Bar', 'Traditional dim sum', 'chinese', 40.7185, -73.9861, 4.5, 6300, 2),
    ('Steakhouse Prime', 'Premium steaks', 'steakhouse', 40.7505, -73.9934, 4.8, 8900, 4),
    ('Green Salad', 'Healthy salads & bowls', 'salad', 40.7395, -73.9950, 4.1, 4200, 2),
    ('Coffee Corner', 'Specialty coffee & pastries', 'cafe', 40.7056, -74.0134, 4.4, 9600, 1)
ON CONFLICT DO NOTHING;

-- Insert sample experiment
INSERT INTO experiments (id, name, description, status, start_date, end_date) VALUES
    ('11111111-1111-1111-1111-111111111111', 'feed_ranking_v1', 'Test different ranking strategies', 'active', NOW(), NOW() + INTERVAL '30 days')
ON CONFLICT (id) DO NOTHING;

-- Insert experiment variants
INSERT INTO variants (id, experiment_id, name, config, traffic_pct) VALUES
    ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'control', '{"popularity": 0.35, "proximity": 0.30, "personalization": 0.25, "rating": 0.10}', 0.25),
    ('22222222-2222-2222-2222-222222222223', '11111111-1111-1111-1111-111111111111', 'popularity-heavy', '{"popularity": 0.55, "proximity": 0.25, "personalization": 0.10, "rating": 0.10}', 0.25),
    ('22222222-2222-2222-2222-222222222224', '11111111-1111-1111-1111-111111111111', 'personalization-heavy', '{"popularity": 0.15, "proximity": 0.20, "personalization": 0.50, "rating": 0.10}', 0.25),
    ('22222222-2222-2222-2222-222222222225', '11111111-1111-1111-1111-111111111111', 'balanced', '{"popularity": 0.30, "proximity": 0.30, "personalization": 0.25, "rating": 0.15}', 0.25)
ON CONFLICT (id) DO NOTHING;
