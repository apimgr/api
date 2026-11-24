package config

// GeoIPConfig holds the operator-configurable settings from AI.md PART 19.
// Database selection and storage paths are fixed by the specification.
type GeoIPConfig struct {
	Enabled bool `yaml:"enabled"`
	// DenyCountries blocks the listed ISO 3166-1 alpha-2 codes and allows
	// every other country.
	DenyCountries []string `yaml:"deny_countries"`
	// AllowCountries allows ONLY the listed codes and blocks the rest. If
	// both lists are set, AllowCountries wins.
	AllowCountries []string `yaml:"allow_countries"`
	// Presets are named operator-authored country lists kept for reuse
	// across the allow/deny fields. They are never applied automatically.
	Presets map[string][]string `yaml:"presets"`
}

// defaultGeoIPConfig returns the PART 19 first-run defaults: enabled, with no
// country blocking.
func defaultGeoIPConfig() GeoIPConfig {
	return GeoIPConfig{
		Enabled:        true,
		DenyCountries:  []string{},
		AllowCountries: []string{},
		Presets:        map[string][]string{},
	}
}
