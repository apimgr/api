package config

// GeoIPConfig holds the GeoIP settings from AI.md PART 19. Databases are
// never embedded in the binary — they are downloaded into Dir on first run
// and refreshed by the scheduler.
type GeoIPConfig struct {
	Enabled bool `yaml:"enabled"`
	// Dir is the directory the downloaded .mmdb files live in. Empty
	// resolves to {data_dir}/security/geoip at runtime.
	Dir string `yaml:"dir"`
	// DenyCountries blocks the listed ISO 3166-1 alpha-2 codes and allows
	// every other country.
	DenyCountries []string `yaml:"deny_countries"`
	// AllowCountries allows ONLY the listed codes and blocks the rest. If
	// both lists are set, AllowCountries wins.
	AllowCountries []string `yaml:"allow_countries"`
	// Presets are named operator-authored country lists kept for reuse
	// across the allow/deny fields. They are never applied automatically.
	Presets map[string][]string `yaml:"presets"`
	// Databases selects which MMDB files are downloaded and consulted.
	Databases GeoIPDatabasesConfig `yaml:"databases"`
}

// GeoIPDatabasesConfig selects the ip-location-db databases in use. All
// three are CC BY 4.0 and require attribution wherever their data is shown.
type GeoIPDatabasesConfig struct {
	// ASN enables autonomous_system_number / _organization lookups.
	ASN bool `yaml:"asn"`
	// Country enables country_code lookups, which country blocking needs.
	Country bool `yaml:"country"`
	// City enables city, state, postcode, lat/lon and timezone lookups.
	City bool `yaml:"city"`
}

// defaultGeoIPConfig returns the PART 19 first-run defaults: enabled, all
// three databases on, and no country blocking at all.
func defaultGeoIPConfig() GeoIPConfig {
	return GeoIPConfig{
		Enabled:        true,
		Dir:            "",
		DenyCountries:  []string{},
		AllowCountries: []string{},
		Presets:        map[string][]string{},
		Databases: GeoIPDatabasesConfig{
			ASN:     true,
			Country: true,
			City:    true,
		},
	}
}
