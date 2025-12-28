package geoip

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// GeoIPDB represents a GeoIP database
type GeoIPDB struct {
	mu      sync.RWMutex
	entries map[string]*GeoIPEntry
	loaded  bool
}

// GeoIPEntry represents a single GeoIP record
type GeoIPEntry struct {
	IP          string
	Country     string
	CountryCode string
	Region      string
	City        string
	Latitude    float64
	Longitude   float64
}

var (
	db     *GeoIPDB
	dbOnce sync.Once
)

// Get returns the singleton GeoIP database
func Get() *GeoIPDB {
	dbOnce.Do(func() {
		db = &GeoIPDB{
			entries: make(map[string]*GeoIPEntry),
		}
	})
	return db
}

// Load loads the GeoIP database from file
func (g *GeoIPDB) Load(dataDir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	dbPath := filepath.Join(dataDir, "geoip", "geoip.csv")

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Printf("GeoIP: Database not found at %s, will download on first request", dbPath)
		return nil
	}

	// Read CSV file
	file, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read all records
	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read GeoIP record: %w", err)
		}

		// Skip header
		if count == 0 && record[0] == "ip" {
			count++
			continue
		}

		// Parse record (format: ip,country_code,country,region,city,lat,lon)
		if len(record) >= 3 {
			lat, _ := strconv.ParseFloat(record[5], 64)
			lon, _ := strconv.ParseFloat(record[6], 64)

			entry := &GeoIPEntry{
				IP:          record[0],
				CountryCode: record[1],
				Country:     record[2],
				Region:      record[3],
				City:        record[4],
				Latitude:    lat,
				Longitude:   lon,
			}

			g.entries[record[0]] = entry
			count++
		}
	}

	g.loaded = true
	log.Printf("GeoIP: Loaded %d entries from database", count)
	return nil
}

// Lookup performs a GeoIP lookup for an IP address
func (g *GeoIPDB) Lookup(ip string) (*GeoIPEntry, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.loaded {
		return nil, fmt.Errorf("GeoIP database not loaded")
	}

	// Normalize IP address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	// Direct lookup
	if entry, ok := g.entries[parsedIP.String()]; ok {
		return entry, nil
	}

	// TODO: Implement CIDR range lookup for better accuracy
	// For now, return unknown
	return &GeoIPEntry{
		IP:          ip,
		Country:     "Unknown",
		CountryCode: "XX",
		Region:      "",
		City:        "",
	}, nil
}

// Download downloads the latest GeoIP database
func Download(dataDir string) error {
	log.Println("GeoIP: Downloading latest database...")

	// Ensure geoip directory exists
	geoipDir := filepath.Join(dataDir, "geoip")
	if err := os.MkdirAll(geoipDir, 0755); err != nil {
		return fmt.Errorf("failed to create geoip directory: %w", err)
	}

	// Download from ip-location-db (free, no API key required)
	// Using dbip-country database
	url := "https://raw.githubusercontent.com/sapics/ip-location-db/main/dbip-country/dbip-country-ipv4.csv"

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download GeoIP database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GeoIP download failed with status: %d", resp.StatusCode)
	}

	// Save to file
	dbPath := filepath.Join(geoipDir, "geoip.csv")
	tmpPath := dbPath + ".tmp"

	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create GeoIP file: %w", err)
	}

	// Copy data
	written, err := io.Copy(file, resp.Body)
	file.Close()

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write GeoIP database: %w", err)
	}

	// Rename temp file to final name (atomic)
	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename GeoIP database: %w", err)
	}

	log.Printf("GeoIP: Downloaded %d bytes to %s", written, dbPath)

	// Reload the database
	return Get().Load(dataDir)
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
