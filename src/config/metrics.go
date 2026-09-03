package config

// MetricsRootConfig controls the unversioned "/metrics" alias, per AI.md
// PART 20 "Endpoints" (default true, matching every other project root
// alias's default-mounted posture).
type MetricsRootConfig struct {
	Enabled bool `yaml:"enabled"`
}

// MetricsAuthTokensConfig holds the per-service bearer tokens checked by
// AuthMiddleware. An empty value disables that specific service (403 on
// every request); this is intentionally separate per service so, for
// example, Prometheus scraping can be enabled while Loki log export stays
// off.
type MetricsAuthTokensConfig struct {
	Prometheus string `yaml:"prometheus"`
	Grafana    string `yaml:"grafana"`
	Loki       string `yaml:"loki"`
}

// MetricsAuthConfig controls authentication for the metrics endpoints.
// AllowUnauthenticated is a firewalled-only escape hatch checked before any
// token comparison - only safe when network-level access control already
// restricts /server/metrics to trusted callers.
type MetricsAuthConfig struct {
	AllowUnauthenticated bool                    `yaml:"allow_unauthenticated"`
	Tokens               MetricsAuthTokensConfig `yaml:"tokens"`
}

// MetricsLokiConfig bounds how much log history the Loki push-API handler
// serves per request.
type MetricsLokiConfig struct {
	MaxEntries int    `yaml:"max_entries"`
	MaxAge     string `yaml:"max_age"`
}
