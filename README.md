# Discovery Feed Service

A Go backend prototype for a Wolt-like discovery feed. It serves ranked venue results, records user interactions, assigns users to ranking experiments, exposes Prometheus metrics, and includes a mock AI workflow for ranking-weight suggestions.

This project is currently a working local prototype, not a production-ready service. The API runs, tests pass, and the main service boundaries are in place, but several analytics and persistence paths are still simplified or stubbed.

## Current Status

What works today:

- HTTP API built with Gin.
- Browser landing page at `/` with links to useful endpoints.
- Feed ranking based on popularity, proximity, personalization defaults, rating, and optional Redis boosts.
- Deterministic A/B experiment assignment.
- Interaction recording for clicks and orders.
- PostgreSQL schema and sample seed data.
- Redis-backed cache/personalization hooks, with degraded startup if Redis is unavailable.
- Prometheus metrics endpoint.
- Mock AI ranking optimizer that returns recommendations and PR-style diffs.
- Unit tests for ranking, experiments, and AI.

Known prototype gaps:

- Experiment result aggregation currently returns example values instead of querying recorded metrics.
- User preference and ranking config JSON fields are not fully round-tripped through Postgres yet.
- Rate limiting middleware is a placeholder.
- AI optimization is mock/rule-based unless a compatible external endpoint is configured.
- Feature flags are set on request context, but not every feature is fully disabled by them.
- Docker Compose is provided, but local direct run may be easier while iterating.

## Architecture

```text
HTTP clients / browser
        |
        v
Gin API handlers
        |
        v
Feed, personalization, experiment, AI services
        |
        v
Ranking engine + repositories
        |
        +--> PostgreSQL: venues, interactions, experiments, configs
        +--> Redis: feed cache, recent behavior, impressions, boosts
        +--> Prometheus: metrics
```

Important directories:

| Path | Purpose |
|------|---------|
| `cmd/api` | Application entrypoint and route setup |
| `internal/handler` | HTTP handlers |
| `internal/service` | Feed and personalization orchestration |
| `internal/ranking` | Ranking formulas and diversity reordering |
| `internal/experiment` | A/B assignment, significance helpers, simulation helpers |
| `internal/repository/postgres` | SQL persistence |
| `internal/repository/redis` | Cache and real-time behavior stores |
| `internal/ai` | Mock/external AI ranking recommendation workflow |
| `internal/observability` | Logging, tracing, and Prometheus metrics |
| `migrations` | Schema and sample data |

## Requirements

- Go 1.22 or newer
- PostgreSQL for the API to serve feed data
- Redis optional; without it the service starts in degraded mode
- Docker + Docker Compose optional for running the full stack

## Run With Docker Compose

```bash
docker compose up -d --build
```

Then open:

- API dashboard: http://localhost:8080/
- Health: http://localhost:8080/health
- Prometheus: http://localhost:9090
- Jaeger: http://localhost:16686
- Grafana: http://localhost:3000

The Compose setup starts API, Postgres, Redis, Jaeger, Prometheus, and Grafana. Postgres loads `migrations/001_initial_schema.sql` on first initialization.

## Run Locally Without Docker

Start or provide a PostgreSQL database, then apply the migration:

```bash
createdb discoveryfeed
psql -d discoveryfeed -f migrations/001_initial_schema.sql
```

Run the API:

```bash
DB_HOST=localhost \
DB_PORT=5432 \
DB_USER=feeduser \
DB_PASSWORD=feedpass \
DB_NAME=discoveryfeed \
FEATURE_TRACING=false \
go run ./cmd/api
```

If Redis is not running, startup logs an error and continues. Feed cache, impressions, recent clicks, and real-time personalization will be unavailable or neutral.

## Useful Endpoints

### Browser / Admin

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/` | Small browser dashboard |
| GET | `/health` | Health response |
| GET | `/metrics` | Prometheus metrics |
| GET | `/api/v1/admin/ranking/weights?variant=control` | Ranking weights for a variant |
| GET | `/api/v1/admin/ranking/variants` | List built-in ranking variants |

### Feed

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/feed` | Generate a ranked venue feed |
| POST | `/api/v1/interactions/click` | Record a click |
| POST | `/api/v1/interactions/order` | Record an order |
| GET | `/api/v1/users/:user_id/analytics` | Basic user feed analytics |

Sample feed request:

```bash
curl "http://localhost:8080/api/v1/feed?user_id=11111111-1111-1111-1111-111111111111&latitude=40.7128&longitude=-74.0060&limit=5"
```

Record a click:

```bash
curl -X POST http://localhost:8080/api/v1/interactions/click \
  -H "Content-Type: application/json" \
  -d '{"user_id":"11111111-1111-1111-1111-111111111111","venue_id":"VENUE_ID_FROM_FEED"}'
```

### Experiments

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/experiments` | List active experiments |
| GET | `/api/v1/experiments/:id/results` | Prototype experiment results response |
| POST | `/api/v1/experiments/significance` | Z-test helper for proportions |
| POST | `/api/v1/experiments/simulate` | Estimate sample size and duration |

### AI

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/ai/health` | AI handler health |
| POST | `/api/v1/ai/optimize` | Mock ranking-weight recommendation |
| POST | `/api/v1/ai/explain` | Explain a supplied recommendation |
| POST | `/api/v1/ai/batch-optimize` | Run recommendations across configs |

Sample AI request:

```bash
curl -X POST http://localhost:8080/api/v1/ai/optimize \
  -H "Content-Type: application/json" \
  -d '{
    "goal": "increase_ctr",
    "current_config": {
      "popularity": 0.35,
      "proximity": 0.30,
      "personalization": 0.25,
      "rating": 0.10,
      "price_match": 0.00
    }
  }'
```

## Ranking Behavior

The default score combines:

```text
score =
  popularity_weight      * popularity_score +
  proximity_weight       * proximity_score +
  personalization_weight * personalization_score +
  rating_weight          * rating_score +
  price_match_weight     * price_match_score +
  small_trending_boost
```

The code intentionally keeps several ranking choices explicit:

- Popularity is capped so one very popular venue cannot dominate the whole feed.
- Proximity uses exponential decay so nearby venues get a strong but not absolute advantage.
- Missing user behavior gets a neutral personalization score.
- Redis boosts are small so real-time spikes influence ties without overwhelming durable signals.
- A diversity pass limits repeated cuisines in top results.

Built-in variants:

- `control`
- `popularity-heavy`
- `personalization-heavy`
- `balanced`
- `rating-focused`
- `advanced-ml`

## Configuration

Configuration is environment-variable based.

| Variable | Default | Notes |
|----------|---------|-------|
| `SERVER_PORT` | `8080` | HTTP port |
| `ENV` | `development` | Uses Gin release mode when `production` |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `feeduser` | PostgreSQL user |
| `DB_PASSWORD` | `feedpass` | PostgreSQL password |
| `DB_NAME` | `discoveryfeed` | PostgreSQL database |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `AI_ENABLED` | `true` | Currently informational for most paths |
| `AI_MOCK_MODE` | `true` | Uses rule-based recommendations |
| `AI_ENDPOINT` | empty | External LLM-compatible endpoint |
| `FEATURE_TRACING` | `true` | Enables OpenTelemetry setup |
| `FEATURE_AB_TESTING` | `true` | Exposed as request context flag |
| `FEATURE_PERSONALIZATION` | `true` | Exposed as request context flag |

## Testing

```bash
go test ./...
```

Current expected result: all packages pass.

Integration script:

```bash
./scripts/integration-test.sh
```

The integration script expects the service and dependencies to be running.

## Observability

The service exposes:

- Structured Zap logs.
- `/metrics` Prometheus endpoint.
- Metrics for feed requests, request duration, ranking score counts, cache hits/misses, DB query duration, AI recommendations, and experiment conversions.
- Optional OpenTelemetry tracing through Jaeger when tracing is enabled and Jaeger is reachable.

## Database

The initial migration creates:

- `venues`
- `user_interactions`
- `user_preferences`
- `experiments`
- `variants`
- `experiment_assignments`
- `experiment_metrics`
- `ranking_configs`

It also inserts sample venues and a sample `feed_ranking_v1` experiment.

## Comments And Code Intent

The most important "why" comments are in the feed and ranking paths:

- Redis is optional so the service can still serve from Postgres.
- Experiment variant is resolved before cache lookup to avoid cross-variant cache pollution.
- Control traffic has a longer cache TTL than experiment variants.
- Only top impressions are recorded because they are most likely to influence user action.
- Popularity caps, distance decay, trend boosts, fatigue, and diversity re-ranking are explained where they affect ranking behavior.
- Experiment assignment explains why hashing is used.

Some comments are still ordinary section labels, such as "Fetch venues from DB". Those are harmless, but the better long-term direction is to remove obvious comments and keep comments for product assumptions, operational tradeoffs, and intentionally incomplete prototype behavior.

## Next Improvements

Highest-value next steps:

1. Persist and load JSON fields correctly for user preferences and ranking configs.
2. Aggregate experiment results from `experiment_metrics`.
3. Replace the placeholder rate limiter with a real in-memory or Redis-backed limiter.
4. Add repository-level integration tests against Postgres.
5. Make feature flags actively change behavior, not only attach context values.
6. Add `rows.Err()` checks in repository query loops.
7. Separate mock AI behavior from external LLM provider behavior more explicitly.
8. Add authentication/authorization before treating admin or AI endpoints as deployable.

## License

MIT
