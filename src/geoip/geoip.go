package geoip

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"
)

// GeoIPDB represents the set of loaded MMDB databases (ASN, country, city).
// Any subset may be nil when its database file is missing or failed to
// load - lookups gracefully degrade per database rather than failing
// outright (see IDEA.md "Failure mode for GeoIP").
type GeoIPDB struct {
	mu         sync.RWMutex
	asnDB      *maxminddb.Reader
	countryDB  *maxminddb.Reader
	cityIPv4DB *maxminddb.Reader
	cityIPv6DB *maxminddb.Reader
	loaded     bool
}

// GeoIPEntry represents the result of a GeoIP lookup for a single IP.
// Fields are populated only when the corresponding database is loaded and
// contains data for the queried address; unavailable fields are left at
// their zero value.
type GeoIPEntry struct {
	IP          string
	Country     string
	CountryCode string
	Region      string
	City        string
	Postcode    string
	State2      string
	Timezone    string
	Latitude    float64
	Longitude   float64
	ASN         uint32
	ASNOrg      string
}

// asnRecord maps the ASN MMDB database_type "asn ipvAll"
type asnRecord struct {
	ASN uint32 `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// countryRecord maps the combined geo-whois-asn-country MMDB
type countryRecord struct {
	CountryCode string `maxminddb:"country_code"`
}

// cityRecord maps the dbip-city MMDB (IPv4/IPv6 split databases)
type cityRecord struct {
	City        string  `maxminddb:"city"`
	CountryCode string  `maxminddb:"country_code"`
	Latitude    float64 `maxminddb:"latitude"`
	Longitude   float64 `maxminddb:"longitude"`
	Postcode    string  `maxminddb:"postcode"`
	State1      string  `maxminddb:"state1"`
	State2      string  `maxminddb:"state2"`
	Timezone    string  `maxminddb:"timezone"`
}

// dbFile describes one downloadable MMDB database
type dbFile struct {
	name string
	url  string
}

// geoipFiles is the exact set of MMDB databases per AI.md PART 19
var geoipFiles = []dbFile{
	{name: "asn.mmdb", url: "https://cdn.jsdelivr.net/npm/@ip-location-db/asn-mmdb/asn.mmdb"},
	{name: "country.mmdb", url: "https://cdn.jsdelivr.net/npm/@ip-location-db/geo-whois-asn-country-mmdb/geo-whois-asn-country.mmdb"},
	{name: "dbip-city-ipv4.mmdb", url: "https://cdn.jsdelivr.net/npm/@ip-location-db/dbip-city-mmdb/dbip-city-ipv4.mmdb"},
	{name: "dbip-city-ipv6.mmdb", url: "https://cdn.jsdelivr.net/npm/@ip-location-db/dbip-city-mmdb/dbip-city-ipv6.mmdb"},
}

var (
	db     *GeoIPDB
	dbOnce sync.Once
)

// Get returns the singleton GeoIP database
func Get() *GeoIPDB {
	dbOnce.Do(func() {
		db = &GeoIPDB{}
	})
	return db
}

// geoipDir returns the directory holding the MMDB files, per AI.md PART 19
// config sample: {data_dir}/security/geoip
func geoipDir(dataDir string) string {
	return filepath.Join(dataDir, "security", "geoip")
}

// Load opens whatever MMDB databases are present on disk. Missing files are
// not an error - GeoIP is a risk signal only, and per IDEA.md the geo/
// network tools must keep returning IP-only results when databases are
// unavailable. All other tools are unaffected.
func (g *GeoIPDB) Load(dataDir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	dir := geoipDir(dataDir)

	g.closeLocked()

	if reader, ok := openIfExists(filepath.Join(dir, "asn.mmdb")); ok {
		g.asnDB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, "country.mmdb")); ok {
		g.countryDB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, "dbip-city-ipv4.mmdb")); ok {
		g.cityIPv4DB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, "dbip-city-ipv6.mmdb")); ok {
		g.cityIPv6DB = reader
	}

	g.loaded = g.asnDB != nil || g.countryDB != nil || g.cityIPv4DB != nil || g.cityIPv6DB != nil

	if !g.loaded {
		log.Printf("GeoIP: no databases found in %s, geo/network tools will return IP only until downloaded", dir)
	}

	return nil
}

// openIfExists opens an MMDB file, logging and returning ok=false on any
// failure (missing file, corrupt file) so that other databases can still
// load.
func openIfExists(path string) (*maxminddb.Reader, bool) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, false
	}

	reader, err := maxminddb.Open(path)
	if err != nil {
		log.Printf("GeoIP: failed to open %s: %v", path, err)
		return nil, false
	}

	return reader, true
}

// closeLocked closes any previously opened databases. Caller must hold g.mu.
func (g *GeoIPDB) closeLocked() {
	for _, reader := range []*maxminddb.Reader{g.asnDB, g.countryDB, g.cityIPv4DB, g.cityIPv6DB} {
		if reader != nil {
			reader.Close()
		}
	}
	g.asnDB = nil
	g.countryDB = nil
	g.cityIPv4DB = nil
	g.cityIPv6DB = nil
}

// Lookup performs a GeoIP lookup for an IP address, joining ASN, country,
// and city data where available. Per AI.md PART 19 (GeoIP is a risk signal,
// never the sole access gate) and IDEA.md's graceful-degradation failure
// mode, a missing or partially loaded database never fails the lookup -
// the entry is simply left with fewer populated fields, falling back to
// IP-only when no database is available at all.
func (g *GeoIPDB) Lookup(ip string) (*GeoIPEntry, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	entry := &GeoIPEntry{IP: parsedIP.String()}

	if g.asnDB != nil {
		var rec asnRecord
		if err := g.asnDB.Lookup(parsedIP, &rec); err == nil {
			entry.ASN = rec.ASN
			entry.ASNOrg = rec.Org
		}
	}

	if g.countryDB != nil {
		var rec countryRecord
		if err := g.countryDB.Lookup(parsedIP, &rec); err == nil && rec.CountryCode != "" {
			entry.CountryCode = rec.CountryCode
			entry.Country = rec.CountryCode
		}
	}

	cityDB := g.cityIPv6DB
	if parsedIP.To4() != nil {
		cityDB = g.cityIPv4DB
	}
	if cityDB != nil {
		var rec cityRecord
		if err := cityDB.Lookup(parsedIP, &rec); err == nil {
			entry.City = rec.City
			entry.Region = rec.State1
			entry.State2 = rec.State2
			entry.Postcode = rec.Postcode
			entry.Timezone = rec.Timezone
			entry.Latitude = rec.Latitude
			entry.Longitude = rec.Longitude
			if entry.CountryCode == "" {
				entry.CountryCode = rec.CountryCode
				entry.Country = rec.CountryCode
			}
		}
	}

	return entry, nil
}

// Download fetches the latest MMDB databases from the ip-location-db CDN
// (per AI.md PART 19) into {data_dir}/security/geoip/ and reloads them.
// Each file is downloaded independently - a failure on one database logs
// a warning and continues with the rest, preserving graceful degradation.
func Download(dataDir string) error {
	log.Println("GeoIP: Downloading latest databases...")

	dir := geoipDir(dataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create geoip directory: %w", err)
	}

	var firstErr error
	downloaded := 0

	for _, f := range geoipFiles {
		if err := downloadFile(f.url, filepath.Join(dir, f.name)); err != nil {
			log.Printf("GeoIP: failed to download %s: %v", f.name, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		downloaded++
	}

	log.Printf("GeoIP: Downloaded %d/%d databases", downloaded, len(geoipFiles))

	if err := Get().Load(dataDir); err != nil {
		return err
	}

	if downloaded == 0 {
		return fmt.Errorf("all GeoIP database downloads failed: %w", firstErr)
	}

	return nil
}

// downloadFile fetches url and atomically writes it to path.
func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	tmpPath := path + ".tmp"

	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	file.Close()

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	log.Printf("GeoIP: Downloaded %d bytes to %s", written, path)
	return nil
}

// IsCountryBlocked checks if a country code is in the block list
func IsCountryBlocked(countryCode string, blocklist []string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(countryCode))
	for _, blocked := range blocklist {
		if strings.ToUpper(strings.TrimSpace(blocked)) == normalized {
			return true
		}
	}
	return false
}
