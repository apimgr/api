package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/health"
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

// DatabaseInfo describes which GeoIP datasets are loaded without exposing
// database records or filesystem paths.
type DatabaseInfo struct {
	Loaded   bool `json:"loaded"`
	ASN      bool `json:"asn"`
	Country  bool `json:"country"`
	CityIPv4 bool `json:"city_ipv4"`
	CityIPv6 bool `json:"city_ipv6"`
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

// countryRecord maps the iptoasn-country MMDB
type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	CountryCode string `maxminddb:"country_code"`
}

func (r countryRecord) code() string {
	if r.Country.ISOCode != "" {
		return r.Country.ISOCode
	}
	return r.CountryCode
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

// MMDB file names on disk. They match the upstream ip-location-db release
// asset names exactly; the country dataset exposes only country_code.
const (
	fileASN      = "iptoasn-asn.mmdb"
	fileCountry  = "iptoasn-country.mmdb"
	fileCityIPv4 = "dbip-city-ipv4.mmdb"
	fileCityIPv6 = "dbip-city-ipv6.mmdb"
	lastUpdated  = ".last_updated"
)

const maxDatabaseAge = 21 * 24 * time.Hour

// dbFile describes one downloadable MMDB database
type dbFile struct {
	name        string
	url         string
	checksumURL string
}

// geoipFiles is the exact set of MMDB databases per AI.md PART 19. There is
// no separate WHOIS database - organization names come from the ASN
// database's autonomous_system_organization field, combined with the country
// database at query time.
var geoipFiles = []dbFile{
	{name: fileASN, url: "https://github.com/sapics/ip-location-db/releases/download/latest/iptoasn-asn.mmdb", checksumURL: "https://github.com/sapics/ip-location-db/releases/download/checksum/iptoasn-asn.mmdb.sha256"},
	{name: fileCountry, url: "https://github.com/sapics/ip-location-db/releases/download/latest/iptoasn-country.mmdb", checksumURL: "https://github.com/sapics/ip-location-db/releases/download/checksum/iptoasn-country.mmdb.sha256"},
	{name: fileCityIPv4, url: "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv4.mmdb", checksumURL: "https://github.com/sapics/ip-location-db/releases/download/checksum/dbip-city-ipv4.mmdb.sha256"},
	{name: fileCityIPv6, url: "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv6.mmdb", checksumURL: "https://github.com/sapics/ip-location-db/releases/download/checksum/dbip-city-ipv6.mmdb.sha256"},
}

var geoipHTTPClient = &http.Client{Timeout: 90 * time.Second}

// ResolveDir returns the fixed PART 19 database directory.
func ResolveDir(_ config.GeoIPConfig, dataDir string) string {
	return geoipDir(dataDir)
}

var (
	db     *GeoIPDB
	dbOnce sync.Once
)

// GetGeoIPDatabase returns the singleton GeoIP database.
func GetGeoIPDatabase() *GeoIPDB {
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

// NeedsInitialDownload reports whether this installation has no successful
// full GeoIP database refresh recorded yet.
func NeedsInitialDownload(dataDir string) bool {
	_, err := os.Stat(filepath.Join(geoipDir(dataDir), lastUpdated))
	return os.IsNotExist(err)
}

// RegisterHealthChecks exposes the freshness of each required GeoIP dataset.
func RegisterHealthChecks(dataDir string) {
	dir := geoipDir(dataDir)
	datasets := []struct {
		key   string
		files []string
	}{
		{key: "asn", files: []string{fileASN}},
		{key: "country", files: []string{fileCountry}},
		{key: "city", files: []string{fileCityIPv4, fileCityIPv6}},
	}
	for _, dataset := range datasets {
		datasetKey, datasetFiles := dataset.key, dataset.files
		health.Register("geoip_"+datasetKey, false, func() string {
			return databaseStatus(dir, datasetFiles, time.Now().UTC())
		})
	}
}

func databaseStatus(dir string, files []string, now time.Time) string {
	for _, name := range files {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() == 0 {
			return "degraded"
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, lastUpdated))
	if err != nil {
		return "degraded"
	}
	updated, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil || now.Sub(updated) > maxDatabaseAge || updated.After(now.Add(time.Minute)) {
		return "degraded"
	}
	return "ok"
}

// Load opens whatever MMDB databases are present on disk. Missing files are
// not an error - GeoIP is a risk signal only, and per IDEA.md the geo/
// network tools must keep returning IP-only results when databases are
// unavailable. All other tools are unaffected.
func (g *GeoIPDB) Load(dataDir string) error {
	return g.loadFrom(geoipDir(dataDir))
}

// LoadFromConfig opens the MMDB databases selected by the server.geoip
// config tree from the operator-configured directory. When GeoIP is disabled
// entirely every database is closed and the package degrades to IP-only
// results, exactly as it does when no database file exists.
func (g *GeoIPDB) LoadFromConfig(geo config.GeoIPConfig, dataDir string) error {
	if !geo.Enabled {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.closeLocked()
		g.loaded = false
		log.Println("GeoIP: disabled by configuration, lookups will return IP only")
		return nil
	}

	if invalid := ValidatePresets(geo); len(invalid) > 0 {
		log.Printf("GeoIP: presets with invalid ISO 3166-1 alpha-2 codes are ignored: %s", strings.Join(invalid, ", "))
	}

	return g.loadFrom(ResolveDir(geo, dataDir))
}

// loadFrom opens every enabled MMDB file found in dir, replacing whatever was
// previously open. Missing and disabled databases are skipped rather than
// treated as errors.
func (g *GeoIPDB) loadFrom(dir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.closeLocked()

	if reader, ok := openIfExists(filepath.Join(dir, fileASN)); ok {
		g.asnDB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, fileCountry)); ok {
		g.countryDB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, fileCityIPv4)); ok {
		g.cityIPv4DB = reader
	}
	if reader, ok := openIfExists(filepath.Join(dir, fileCityIPv6)); ok {
		g.cityIPv6DB = reader
	}

	g.loaded = g.asnDB != nil || g.countryDB != nil || g.cityIPv4DB != nil || g.cityIPv6DB != nil

	if !g.loaded {
		log.Printf("GeoIP: no databases found in %s, geo/network tools will return IP only until downloaded", dir)
	}

	return nil
}

// HasCountryDB reports whether the country database is loaded. Country
// blocking is skipped (fail-open) whenever this is false.
func (g *GeoIPDB) HasCountryDB() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.countryDB != nil
}

// GetDatabaseInfo returns metadata only; MMDB contents and on-disk paths
// are never copied into the cache.
func (g *GeoIPDB) GetDatabaseInfo() DatabaseInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return DatabaseInfo{
		Loaded:   g.loaded,
		ASN:      g.asnDB != nil,
		Country:  g.countryDB != nil,
		CityIPv4: g.cityIPv4DB != nil,
		CityIPv6: g.cityIPv6DB != nil,
	}
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
			_ = reader.Close()
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
		if err := g.countryDB.Lookup(parsedIP, &rec); err == nil && rec.code() != "" {
			entry.CountryCode = rec.code()
			entry.Country = rec.code()
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
	return downloadInto(geoipDir(dataDir))
}

// DownloadFromConfig fetches only the databases enabled by the server.geoip
// config tree into the operator-configured directory. It is a no-op when
// GeoIP is disabled, so the scheduler's weekly refresh task never downloads
// data the operator turned off.
func DownloadFromConfig(geo config.GeoIPConfig, dataDir string) error {
	if !geo.Enabled {
		log.Println("GeoIP: disabled by configuration, skipping database download")
		return nil
	}

	return downloadInto(ResolveDir(geo, dataDir))
}

// downloadInto fetches every enabled MMDB file into dir and reloads the
// singleton from it. Each file is downloaded independently - a failure on one
// database logs a warning and continues with the rest, preserving graceful
// degradation.
func downloadInto(dir string) error {
	defer health.Refresh()
	log.Println("GeoIP: Downloading latest databases...")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create geoip directory: %w", err)
	}

	var firstErr error
	downloaded := 0
	selected := 0

	for _, f := range geoipFiles {
		selected++

		if err := downloadWithRecovery(f, filepath.Join(dir, f.name)); err != nil {
			log.Printf("GeoIP: failed to download %s: %v", f.name, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		downloaded++
	}

	log.Printf("GeoIP: Downloaded %d/%d databases", downloaded, selected)

	if err := GetGeoIPDatabase().loadFrom(dir); err != nil {
		return err
	}

	if downloaded != selected {
		markerPath := filepath.Join(dir, lastUpdated)
		if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
			firstErr = errors.Join(firstErr, fmt.Errorf("mark GeoIP data degraded: %w", err))
		}
		if firstErr != nil {
			return fmt.Errorf("downloaded %d of %d GeoIP databases: %w", downloaded, selected, firstErr)
		}
		return fmt.Errorf("downloaded %d of %d GeoIP databases", downloaded, selected)
	}
	if err := os.WriteFile(filepath.Join(dir, lastUpdated), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0600); err != nil {
		return fmt.Errorf("write GeoIP update marker: %w", err)
	}

	return nil
}

func downloadVerifiedFile(sourceURL, checksumURL, target string) error {
	tmpPath, verified, err := fetchGeoIPCandidate(sourceURL, checksumURL, target)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if !verified {
		return errors.New("downloaded database checksum mismatch")
	}
	if err := replaceFileAtomically(tmpPath, target); err != nil {
		return fmt.Errorf("failed to replace database: %w", err)
	}
	return nil
}

func downloadWithRecovery(file dbFile, target string) error {
	var lastCandidate string
	for attempt := 0; attempt < 2; attempt++ {
		tmpPath, verified, err := fetchGeoIPCandidate(file.url, file.checksumURL, target)
		if err != nil {
			if lastCandidate != "" {
				_ = os.Remove(lastCandidate)
			}
			return err
		}
		if verified {
			if lastCandidate != "" {
				_ = os.Remove(lastCandidate)
			}
			defer func() { _ = os.Remove(tmpPath) }()
			if err := replaceFileAtomically(tmpPath, target); err != nil {
				return fmt.Errorf("failed to replace database: %w", err)
			}
			return nil
		}
		if attempt == 0 {
			lastCandidate = tmpPath
			time.Sleep(250 * time.Millisecond)
			continue
		}

		if hasValidMMDB(target) {
			_ = os.Remove(lastCandidate)
			_ = os.Remove(tmpPath)
			return errors.New("checksum mismatch after retry; keeping the live database")
		}
		if err := validateMMDB(tmpPath); err != nil {
			_ = os.Remove(lastCandidate)
			_ = os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch after retry and unverified database is invalid: %w", err)
		}
		if err := replaceFileAtomically(tmpPath, target); err != nil {
			_ = os.Remove(lastCandidate)
			return fmt.Errorf("install unverified database: %w", err)
		}
		_ = os.Remove(lastCandidate)
		log.Printf("GeoIP: WARNING installed %s without checksum verification after retry because no valid live database existed", file.name)
		return errors.New("checksum mismatch after retry; installed a valid unverified database and marked the dataset degraded")
	}
	return errors.New("checksum verification failed")
}

func fetchGeoIPCandidate(sourceURL, checksumURL, target string) (string, bool, error) {
	checksumResponse, err := geoipHTTPClient.Get(checksumURL)
	if err != nil {
		return "", false, fmt.Errorf("checksum download failed: %w", err)
	}
	defer func() {
		if err := checksumResponse.Body.Close(); err != nil {
			log.Printf("GeoIP: failed to close checksum response: %v", err)
		}
	}()
	if checksumResponse.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("checksum download failed with status: %d", checksumResponse.StatusCode)
	}
	checksumBody, err := io.ReadAll(io.LimitReader(checksumResponse.Body, 4096))
	if err != nil {
		return "", false, fmt.Errorf("checksum read failed: %w", err)
	}
	fields := strings.Fields(string(checksumBody))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", false, errors.New("checksum file has invalid format")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil {
		return "", false, fmt.Errorf("checksum file has invalid SHA-256: %w", err)
	}

	response, err := geoipHTTPClient.Get(sourceURL)
	if err != nil {
		return "", false, fmt.Errorf("download failed: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("GeoIP: failed to close download response: %v", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("download failed with status: %d", response.StatusCode)
	}

	file, err := os.CreateTemp(filepath.Dir(target), ".geoip-*.tmp")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := file.Name()
	keepCandidate := false
	defer func() {
		if !keepCandidate {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return "", false, fmt.Errorf("failed to set temporary file permissions: %w", err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hasher), response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return "", false, fmt.Errorf("failed to write downloaded database: %w", copyErr)
	}
	if closeErr != nil {
		return "", false, fmt.Errorf("failed to close downloaded database: %w", closeErr)
	}
	keepCandidate = true
	return tmpPath, equalSHA256(hasher.Sum(nil), want), nil
}

func hasValidMMDB(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return validateMMDB(path) == nil
}

func validateMMDB(path string) error {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return err
	}
	metadata := reader.Metadata
	closeErr := reader.Close()
	if closeErr != nil {
		return closeErr
	}
	if metadata.DatabaseType == "" || (metadata.IPVersion != 4 && metadata.IPVersion != 6) ||
		(metadata.RecordSize != 24 && metadata.RecordSize != 28 && metadata.RecordSize != 32) ||
		metadata.BinaryFormatMajorVersion != 2 || metadata.BuildEpoch == 0 {
		return errors.New("MMDB metadata is incomplete or outside supported values")
	}
	return nil
}

func equalSHA256(left, right []byte) bool {
	if len(left) != sha256.Size || len(right) != sha256.Size {
		return false
	}
	var difference byte
	for i := range left {
		difference |= left[i] ^ right[i]
	}
	return difference == 0
}

// IsCountryBlocked checks if a country code is in the block list
func IsCountryBlocked(countryCode string, blocklist []string) bool {
	return containsCountry(blocklist, countryCode)
}

// containsCountry reports whether list holds countryCode, comparing
// case-insensitively with surrounding whitespace trimmed on both sides. An
// empty country code never matches.
func containsCountry(list []string, countryCode string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(countryCode))
	if normalized == "" {
		return false
	}

	for _, candidate := range list {
		if strings.ToUpper(strings.TrimSpace(candidate)) == normalized {
			return true
		}
	}

	return false
}
