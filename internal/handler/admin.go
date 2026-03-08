package handler

import (
	"net/http"
	"strings"

	"github.com/discovery-feed-service/internal/ranking"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	rankingEngine *ranking.Engine
}

func NewAdminHandler(engine *ranking.Engine) *AdminHandler {
	return &AdminHandler{rankingEngine: engine}
}

func (h *AdminHandler) ServiceInfo(c *gin.Context) {
	if strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Discovery Feed Service</title>
  <style>
    :root { color-scheme: light dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f7f8fb; color: #18202f; }
    main { max-width: 900px; margin: 0 auto; padding: 48px 24px; }
    h1 { margin: 0 0 8px; font-size: 36px; font-weight: 750; letter-spacing: 0; }
    p { margin: 0; color: #526071; line-height: 1.55; }
    .status { display: inline-flex; align-items: center; gap: 8px; margin: 24px 0 32px; padding: 8px 12px; border: 1px solid #cdd8e5; border-radius: 8px; background: #fff; font-weight: 650; }
    .dot { width: 10px; height: 10px; border-radius: 999px; background: #16a34a; box-shadow: 0 0 0 4px #dcfce7; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; }
    a { display: block; min-height: 86px; padding: 16px; border: 1px solid #d9e1ec; border-radius: 8px; background: #fff; color: inherit; text-decoration: none; }
    a:hover { border-color: #6b8afd; box-shadow: 0 8px 24px rgba(24, 32, 47, .08); transform: translateY(-1px); }
    strong { display: block; margin-bottom: 6px; font-size: 16px; }
    code { color: #46566b; font-size: 13px; overflow-wrap: anywhere; }
    @media (prefers-color-scheme: dark) {
      body { background: #10141c; color: #edf2f7; }
      p, code { color: #aeb9c8; }
      .status, a { background: #161c27; border-color: #293244; }
      .dot { background: #22c55e; box-shadow: 0 0 0 4px rgba(34, 197, 94, .18); }
      a:hover { border-color: #8aa0ff; box-shadow: 0 8px 24px rgba(0, 0, 0, .25); }
    }
  </style>
</head>
<body>
  <main>
    <h1>Discovery Feed Service</h1>
    <p>Local backend is running. Use these entry points to inspect the feed, experiments, ranking variants, health, and metrics.</p>
    <div class="status"><span class="dot"></span> running on localhost:8080</div>
    <section class="grid">
      <a href="/health"><strong>Health</strong><code>/health</code></a>
      <a href="/api/v1/feed?user_id=11111111-1111-1111-1111-111111111111&amp;latitude=40.7128&amp;longitude=-74.0060&amp;limit=5"><strong>Sample Feed</strong><code>/api/v1/feed?...limit=5</code></a>
      <a href="/api/v1/experiments"><strong>Experiments</strong><code>/api/v1/experiments</code></a>
      <a href="/api/v1/admin/ranking/variants"><strong>Ranking Variants</strong><code>/api/v1/admin/ranking/variants</code></a>
      <a href="/api/v1/ai/health"><strong>AI Health</strong><code>/api/v1/ai/health</code></a>
      <a href="/metrics"><strong>Prometheus Metrics</strong><code>/metrics</code></a>
    </section>
  </main>
</body>
</html>`)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service": "discovery-feed",
		"status":  "running",
		"endpoints": gin.H{
			"health":       "/health",
			"feed":         "/api/v1/feed?user_id=11111111-1111-1111-1111-111111111111&latitude=40.7128&longitude=-74.0060&limit=5",
			"experiments":  "/api/v1/experiments",
			"ai_health":    "/api/v1/ai/health",
			"ranking_info": "/api/v1/admin/ranking/variants",
			"metrics":      "/metrics",
		},
	})
}

func (h *AdminHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "discovery-feed",
		"version": "1.0.0",
		"features": gin.H{
			"personalization": true,
			"ab_testing":      true,
			"ai_optimization": true,
			"tracing":         true,
		},
	})
}

func (h *AdminHandler) GetRankingWeights(c *gin.Context) {
	variant := c.Query("variant")
	if variant == "" {
		variant = "control"
	}

	weights := h.rankingEngine.GetWeightsForVariant(variant)

	c.JSON(http.StatusOK, gin.H{
		"variant": variant,
		"weights": weights,
	})
}

func (h *AdminHandler) ListVariants(c *gin.Context) {
	variants := []string{
		"control",
		"popularity-heavy",
		"personalization-heavy",
		"balanced",
		"rating-focused",
		"advanced-ml",
	}

	result := make([]gin.H, 0, len(variants))
	for _, v := range variants {
		weights := h.rankingEngine.GetWeightsForVariant(v)
		result = append(result, gin.H{
			"name":    v,
			"weights": weights,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"variants": result,
	})
}

func (h *AdminHandler) GetMetrics(c *gin.Context) {
	// Prometheus metrics are served on a separate endpoint
	c.JSON(http.StatusOK, gin.H{
		"message": "metrics available at /metrics",
	})
}
