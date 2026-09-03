package geoip

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MaxMind DB's own data encoding is NOT standard msgpack: the top 3 bits of
// each control byte give a MaxMind-specific type number (map=7, string=2,
// uint32=6, ...) and the low 5 bits give the size, with types numbered 8+
// (e.g. array/slice=11) requiring an "extended" control byte pair. These
// mmdbWrite* helpers follow that scheme (verified against
// github.com/oschwald/maxminddb-golang@v1.13.1's decoder.go) so the
// synthetic metadata built below actually parses as a valid Metadata value.

// mmdbWriteString appends a MaxMind DB string (type 2, len < 29) to buf.
func mmdbWriteString(buf *bytes.Buffer, s string) {
	buf.WriteByte(byte(0x40 | len(s)))
	buf.WriteString(s)
}

// mmdbWriteMap appends a MaxMind DB map header (type 7, n < 29 entries) to buf.
func mmdbWriteMap(buf *bytes.Buffer, n int) {
	buf.WriteByte(byte(0xE0 | n))
}

// mmdbWriteArray appends a MaxMind DB array/slice header (extended type 11,
// n < 29 elements) to buf.
func mmdbWriteArray(buf *bytes.Buffer, n int) {
	buf.WriteByte(byte(n))
	buf.WriteByte(4)
}

// mmdbWriteUint32 appends a MaxMind DB uint32 (type 6) to buf, using the
// minimal big-endian byte representation of v (zero bytes for v == 0), which
// is how the format encodes all unsigned integer types.
func mmdbWriteUint32(buf *bytes.Buffer, v uint32) {
	var data []byte
	for v > 0 {
		data = append([]byte{byte(v & 0xff)}, data...)
		v >>= 8
	}
	buf.WriteByte(byte(0xC0 | len(data)))
	buf.Write(data)
}

// buildMinimalMMDB constructs the smallest possible well-formed MaxMind DB
// file: an empty search tree (node_count=0), an empty data section, and a
// minimal metadata map. Every lookup against it returns "not found" (nil
// error, untouched result) without requiring any real geo data, letting
// Load/Lookup's "database present and opened" branches be exercised without
// a real downloaded MMDB fixture (per project policy, real GeoIP databases
// are never embedded/committed - they are downloaded at runtime).
func buildMinimalMMDB(t *testing.T, ipVersion uint8) []byte {
	t.Helper()

	var meta bytes.Buffer
	mmdbWriteMap(&meta, 9)

	mmdbWriteString(&meta, "node_count")
	mmdbWriteUint32(&meta, 0)

	mmdbWriteString(&meta, "record_size")
	mmdbWriteUint32(&meta, 24)

	mmdbWriteString(&meta, "ip_version")
	mmdbWriteUint32(&meta, uint32(ipVersion))

	mmdbWriteString(&meta, "database_type")
	mmdbWriteString(&meta, "Test")

	mmdbWriteString(&meta, "languages")
	mmdbWriteArray(&meta, 0)

	mmdbWriteString(&meta, "binary_format_major_version")
	mmdbWriteUint32(&meta, 2)

	mmdbWriteString(&meta, "binary_format_minor_version")
	mmdbWriteUint32(&meta, 0)

	mmdbWriteString(&meta, "build_epoch")
	mmdbWriteUint32(&meta, 1)

	mmdbWriteString(&meta, "description")
	mmdbWriteMap(&meta, 1)
	mmdbWriteString(&meta, "en")
	mmdbWriteString(&meta, "Test")

	var file bytes.Buffer
	// data separator (search tree is empty since node_count=0, so this
	// starts the file); data section is also empty.
	file.Write(make([]byte, 16))
	file.WriteString("\xAB\xCD\xEFMaxMind.com")
	file.Write(meta.Bytes())

	return file.Bytes()
}

func TestGet_ReturnsSingleton(t *testing.T) {
	g1 := Get()
	g2 := Get()
	assert.Same(t, g1, g2)
}

func TestGeoipDir(t *testing.T) {
	assert.Equal(t, filepath.Join("/data", "security", "geoip"), geoipDir("/data"))
}

func TestLoad_NoFilesPresent(t *testing.T) {
	dir := t.TempDir()
	g := &GeoIPDB{}

	err := g.Load(dir)
	require.NoError(t, err)
	assert.False(t, g.loaded)
	assert.Nil(t, g.asnDB)
	assert.Nil(t, g.countryDB)
	assert.Nil(t, g.cityIPv4DB)
	assert.Nil(t, g.cityIPv6DB)
}

func TestLoad_CorruptFileIgnored(t *testing.T) {
	dir := t.TempDir()
	geoDir := geoipDir(dir)
	require.NoError(t, os.MkdirAll(geoDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileASN), []byte("not a real mmdb"), 0644))

	g := &GeoIPDB{}
	err := g.Load(dir)
	require.NoError(t, err)
	assert.False(t, g.loaded)
	assert.Nil(t, g.asnDB)
}

func TestLoad_ClosesPreviouslyLoadedDBs(t *testing.T) {
	dir := t.TempDir()
	g := &GeoIPDB{}

	// First load with nothing present.
	require.NoError(t, g.Load(dir))
	assert.False(t, g.loaded)

	// A second load must not panic even though nothing was previously
	// open, exercising closeLocked's nil-safe loop.
	require.NoError(t, g.Load(dir))
	assert.False(t, g.loaded)
}

func TestLookup_InvalidIP(t *testing.T) {
	g := &GeoIPDB{}

	entry, err := g.Lookup("not-an-ip")
	require.Error(t, err)
	assert.Nil(t, entry)
	assert.Contains(t, err.Error(), "invalid IP address")
}

func TestLookup_NoDatabasesLoadedReturnsIPOnly(t *testing.T) {
	g := &GeoIPDB{}

	entry, err := g.Lookup("8.8.8.8")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "8.8.8.8", entry.IP)
	assert.Equal(t, "", entry.Country)
	assert.Equal(t, uint32(0), entry.ASN)
}

func TestLookup_IPv6NoDatabasesLoaded(t *testing.T) {
	g := &GeoIPDB{}

	entry, err := g.Lookup("2001:4860:4860::8888")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "2001:4860:4860::8888", entry.IP)
}

func TestOpenIfExists_MissingFile(t *testing.T) {
	dir := t.TempDir()
	reader, ok := openIfExists(filepath.Join(dir, "missing.mmdb"))
	assert.False(t, ok)
	assert.Nil(t, reader)
}

func TestOpenIfExists_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.mmdb")
	require.NoError(t, os.WriteFile(path, []byte("garbage"), 0644))

	reader, ok := openIfExists(path)
	assert.False(t, ok)
	assert.Nil(t, reader)
}

func TestCloseLocked_NilSafe(t *testing.T) {
	g := &GeoIPDB{}
	// Must not panic when every DB pointer is nil.
	g.closeLocked()
	assert.Nil(t, g.asnDB)
	assert.Nil(t, g.countryDB)
	assert.Nil(t, g.cityIPv4DB)
	assert.Nil(t, g.cityIPv6DB)
}

func TestIsCountryBlocked(t *testing.T) {
	tests := []struct {
		name      string
		country   string
		blocklist []string
		want      bool
	}{
		{"exact match", "US", []string{"US", "CN"}, true},
		{"case insensitive", "us", []string{"US"}, true},
		{"whitespace trimmed on input", "  US  ", []string{"US"}, true},
		{"whitespace trimmed in list", "US", []string{"  US  "}, true},
		{"not in list", "FR", []string{"US", "CN"}, false},
		{"empty blocklist", "US", []string{}, false},
		{"nil blocklist", "US", nil, false},
		{"empty country", "", []string{"US"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCountryBlocked(tt.country, tt.blocklist))
		})
	}
}

func TestDownloadFile_Success(t *testing.T) {
	body := []byte("fake-mmdb-content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "out.mmdb")

	err := downloadFile(srv.URL, target)
	require.NoError(t, err)

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, body, got)

	// The .tmp staging file must not be left behind.
	_, statErr := os.Stat(target + ".tmp")
	assert.True(t, os.IsNotExist(statErr))
}

func TestDownloadFile_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "out.mmdb")

	err := downloadFile(srv.URL, target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status: 404")

	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr))
}

func TestDownloadFile_UnreachableHost(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.mmdb")

	err := downloadFile("http://127.0.0.1:1/no-such-server", target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

// Download's MkdirAll failure path is exercised by pointing dataDir at a
// location where the required geoip subdirectory cannot be created (its
// parent already exists as a regular file, not a directory).
func TestDownload_MkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "security")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0644))

	err := Download(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create geoip directory")
}

// Download's real network path (fetching from the live ip-location-db CDN)
// is not exercised here - it requires outbound internet access, which is
// unavailable/unreliable inside the sandboxed test container per project
// testing rules (mock/skip cleanly rather than depending on a real
// network fetch or a real MMDB file).
func TestDownload_NetworkPath(t *testing.T) {
	t.Skip("requires outbound network access to the real ip-location-db CDN; not available in sandboxed test container")
}

// Load and Lookup both exercise their "database present and opened"
// branches using a minimal synthetic MMDB (empty search tree, node_count=0)
// built entirely in-memory via buildMinimalMMDB, so no real GeoIP database
// file or external MMDB-writer dependency is required. An empty database
// always reports "not found" (nil error, untouched result) for every IP,
// which is exactly the code path geoip.go already relies on in production
// when a real database has no entry for the queried address.
func TestLoad_AllDatabasesPresent(t *testing.T) {
	dir := t.TempDir()
	geoDir := geoipDir(dir)
	require.NoError(t, os.MkdirAll(geoDir, 0755))

	ipv4 := buildMinimalMMDB(t, 4)
	ipv6 := buildMinimalMMDB(t, 6)

	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileASN), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCountry), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCityIPv4), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCityIPv6), ipv6, 0644))

	g := &GeoIPDB{}
	require.NoError(t, g.Load(dir))
	assert.True(t, g.loaded)
	assert.NotNil(t, g.asnDB)
	assert.NotNil(t, g.countryDB)
	assert.NotNil(t, g.cityIPv4DB)
	assert.NotNil(t, g.cityIPv6DB)

	entry, err := g.Lookup("8.8.8.8")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "8.8.8.8", entry.IP)

	entry, err = g.Lookup("2001:4860:4860::8888")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "2001:4860:4860::8888", entry.IP)
}

// countryMMDBWithReader returns a GeoIPDB whose country database pointer is
// populated with a minimal synthetic MMDB. Every lookup against it reports
// "not found", so it exercises the "country database available but no data"
// path; tests needing a concrete country instead stub CountryCodeOf's inputs
// through the deny/allow list logic directly.
func loadedCountryDB(t *testing.T) *GeoIPDB {
	t.Helper()

	dir := t.TempDir()
	geoDir := geoipDir(dir)
	require.NoError(t, os.MkdirAll(geoDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCountry), buildMinimalMMDB(t, 4), 0644))

	g := &GeoIPDB{}
	require.NoError(t, g.Load(dir))
	require.True(t, g.HasCountryDB())

	return g
}

// enabledGeoIP builds a GeoIP config with all databases on and the supplied
// allow/deny lists, matching the PART 19 first-run defaults otherwise.
func enabledGeoIP(deny, allow []string) config.GeoIPConfig {
	return config.GeoIPConfig{
		Enabled:        true,
		DenyCountries:  deny,
		AllowCountries: allow,
		Presets:        map[string][]string{},
		Databases: config.GeoIPDatabasesConfig{
			ASN:     true,
			Country: true,
			City:    true,
		},
	}
}

func TestIsValidCountryCode(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"US", true},
		{"us", true},
		{" CA ", true},
		{"USA", false},
		{"U", false},
		{"", false},
		{"U1", false},
		{"12", false},
		{"U-", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.want, IsValidCountryCode(tt.code))
		})
	}
}

func TestNormalizeCountryCodes(t *testing.T) {
	got := NormalizeCountryCodes([]string{" us ", "CN", "us", "", "USA", "1A", "ru"})
	assert.Equal(t, []string{"US", "CN", "RU"}, got)
	assert.Empty(t, NormalizeCountryCodes(nil))
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"172.16.4.9", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"::1", true},
		{"fd00::1", true},
		{"fe80::1", true},
		{"0.0.0.0", true},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			assert.Equal(t, tt.want, IsPrivateIP(net.ParseIP(tt.ip)))
		})
	}

	assert.False(t, IsPrivateIP(nil))
}

func TestIsAllowlisted(t *testing.T) {
	allowlist := []config.AllowlistEntry{
		{CIDR: "203.0.113.0/24"},
		{CIDR: "198.51.100.7"},
		{CIDR: "2001:db8::/32"},
		{CIDR: "not-an-ip"},
		{CIDR: "999.1.1.1/24"},
		{CIDR: "   "},
	}

	assert.True(t, IsAllowlisted(net.ParseIP("203.0.113.55"), allowlist))
	assert.True(t, IsAllowlisted(net.ParseIP("198.51.100.7"), allowlist))
	assert.True(t, IsAllowlisted(net.ParseIP("2001:db8::5"), allowlist))
	assert.False(t, IsAllowlisted(net.ParseIP("8.8.8.8"), allowlist))
	assert.False(t, IsAllowlisted(net.ParseIP("198.51.100.8"), allowlist))
	assert.False(t, IsAllowlisted(nil, allowlist))
	assert.False(t, IsAllowlisted(net.ParseIP("8.8.8.8"), nil))
}

func TestResolvePreset(t *testing.T) {
	geo := enabledGeoIP(nil, nil)
	geo.Presets = map[string][]string{
		"sanctioned": {"cn", " ru ", "bogus"},
	}

	codes, ok := ResolvePreset(geo, " sanctioned ")
	require.True(t, ok)
	assert.Equal(t, []string{"CN", "RU"}, codes)

	_, ok = ResolvePreset(geo, "missing")
	assert.False(t, ok)

	geo.Presets = nil
	_, ok = ResolvePreset(geo, "sanctioned")
	assert.False(t, ok)
}

// A preset must never influence enforcement on its own - only the
// deny_countries/allow_countries fields do, and they stay empty by default.
func TestCheckCountry_PresetsAreNeverAutoApplied(t *testing.T) {
	g := loadedCountryDB(t)

	geo := enabledGeoIP(nil, nil)
	geo.Presets = map[string][]string{"blocked": {"US", "CN", "RU"}}

	decision := g.CheckCountry("8.8.8.8", geo, nil)
	assert.False(t, decision.Blocked)
	assert.Equal(t, ReasonNoRules, decision.Reason)
}

func TestCheckCountry_DisabledSkipsCheck(t *testing.T) {
	g := loadedCountryDB(t)

	geo := enabledGeoIP([]string{"US"}, nil)
	geo.Enabled = false

	decision := g.CheckCountry("8.8.8.8", geo, nil)
	assert.False(t, decision.Blocked)
	assert.Equal(t, ReasonDisabled, decision.Reason)
}

func TestCheckCountry_InvalidIPFailsOpen(t *testing.T) {
	g := loadedCountryDB(t)

	decision := g.CheckCountry("not-an-ip", enabledGeoIP([]string{"US"}, nil), nil)
	assert.False(t, decision.Blocked)
	assert.Equal(t, ReasonInvalidIP, decision.Reason)
}

// RFC 1918 / RFC 4193 / loopback addresses are never looked up or blocked,
// even under allow_countries mode which blocks every unknown country.
func TestCheckCountry_PrivateIPsAreNeverBlocked(t *testing.T) {
	g := loadedCountryDB(t)

	geo := enabledGeoIP(nil, []string{"US"})

	for _, ip := range []string{"10.1.2.3", "172.20.0.1", "192.168.5.5", "127.0.0.1", "::1", "fd00::1"} {
		t.Run(ip, func(t *testing.T) {
			decision := g.CheckCountry(ip, geo, nil)
			assert.False(t, decision.Blocked)
			assert.Equal(t, ReasonPrivateIP, decision.Reason)
			assert.Empty(t, decision.CountryCode)
		})
	}
}

// server.security.allowlist entries bypass country blocking entirely, in
// both allow and deny modes.
func TestCheckCountry_AllowlistBypassesBlocking(t *testing.T) {
	g := loadedCountryDB(t)

	allowlist := []config.AllowlistEntry{{CIDR: "203.0.113.0/24"}}

	for _, geo := range []config.GeoIPConfig{
		enabledGeoIP([]string{"US", "CN"}, nil),
		enabledGeoIP(nil, []string{"DE"}),
	} {
		decision := g.CheckCountry("203.0.113.9", geo, allowlist)
		assert.False(t, decision.Blocked)
		assert.Equal(t, ReasonAllowlisted, decision.Reason)
	}
}

// Country blocking requires the country database; without it the check is
// skipped rather than blocking the request.
func TestCheckCountry_MissingDatabaseFailsOpen(t *testing.T) {
	g := &GeoIPDB{}
	require.False(t, g.HasCountryDB())

	denyDecision := g.CheckCountry("8.8.8.8", enabledGeoIP([]string{"US"}, nil), nil)
	assert.False(t, denyDecision.Blocked)
	assert.Equal(t, ReasonNoCountryDatabase, denyDecision.Reason)

	allowDecision := g.CheckCountry("8.8.8.8", enabledGeoIP(nil, []string{"DE"}), nil)
	assert.False(t, allowDecision.Blocked)
	assert.Equal(t, ReasonNoCountryDatabase, allowDecision.Reason)
}

// Disabling the country database in config has the same fail-open effect as
// the file being absent.
func TestCheckCountry_CountryDatabaseDisabledFailsOpen(t *testing.T) {
	g := loadedCountryDB(t)

	geo := enabledGeoIP([]string{"US"}, nil)
	geo.Databases.Country = false

	decision := g.CheckCountry("8.8.8.8", geo, nil)
	assert.False(t, decision.Blocked)
	assert.Equal(t, ReasonNoCountryDatabase, decision.Reason)
}

// A loaded database with no entry for the address yields an unknown country,
// which is never grounds for blocking.
func TestCheckCountry_UnknownCountryFailsOpen(t *testing.T) {
	g := loadedCountryDB(t)

	decision := g.CheckCountry("8.8.8.8", enabledGeoIP(nil, []string{"US"}), nil)
	assert.False(t, decision.Blocked)
	assert.Equal(t, ReasonNoCountryData, decision.Reason)
	assert.Empty(t, decision.CountryCode)
}

// TestCountryRulePrecedence drives the same rule evaluator CheckCountry uses
// once a country has been resolved, so allow/deny precedence is asserted
// against production logic without committing a real geo database.
func TestCountryRulePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		deny       []string
		allow      []string
		wantBlock  bool
		wantReason string
	}{
		{"no rules allows everything", "CN", nil, nil, false, ReasonNoRules},
		{"deny list blocks listed", "CN", []string{"CN", "RU"}, nil, true, ReasonCountryDenied},
		{"deny list allows unlisted", "US", []string{"CN", "RU"}, nil, false, ReasonCountryNotDenied},
		{"allow list permits listed", "US", nil, []string{"US", "CA"}, false, ReasonCountryAllowlisted},
		{"allow list blocks unlisted", "CN", nil, []string{"US", "CA"}, true, ReasonCountryNotAllowlisted},
		{"allow wins over deny for listed country", "US", []string{"US"}, []string{"US"}, false, ReasonCountryAllowlisted},
		{"allow wins over deny for country absent from both", "DE", []string{"CN"}, []string{"US"}, true, ReasonCountryNotAllowlisted},
		{"lists are case insensitive", "US", nil, []string{"us"}, false, ReasonCountryAllowlisted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := evaluateCountryRules(
				tt.code,
				NormalizeCountryCodes(tt.allow),
				NormalizeCountryCodes(tt.deny),
			)
			assert.Equal(t, tt.wantBlock, decision.Blocked)
			assert.Equal(t, tt.wantReason, decision.Reason)
		})
	}
}

func TestResolveDir(t *testing.T) {
	geo := enabledGeoIP(nil, nil)
	assert.Equal(t, filepath.Join("/data", "security", "geoip"), ResolveDir(geo, "/data"))

	geo.Dir = "  /custom/geoip  "
	assert.Equal(t, "/custom/geoip", ResolveDir(geo, "/data"))
}

func TestLoadFromConfig_DisabledClosesEverything(t *testing.T) {
	g := loadedCountryDB(t)
	require.True(t, g.HasCountryDB())

	geo := enabledGeoIP(nil, nil)
	geo.Enabled = false

	require.NoError(t, g.LoadFromConfig(geo, t.TempDir()))
	assert.False(t, g.loaded)
	assert.False(t, g.HasCountryDB())
}

// Only the databases selected in server.geoip.databases are opened.
func TestLoadFromConfig_HonorsDatabaseSelection(t *testing.T) {
	dir := t.TempDir()
	geoDir := geoipDir(dir)
	require.NoError(t, os.MkdirAll(geoDir, 0755))

	ipv4 := buildMinimalMMDB(t, 4)
	ipv6 := buildMinimalMMDB(t, 6)
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileASN), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCountry), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCityIPv4), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, fileCityIPv6), ipv6, 0644))

	geo := enabledGeoIP(nil, nil)
	geo.Databases.ASN = false
	geo.Databases.City = false

	g := &GeoIPDB{}
	require.NoError(t, g.LoadFromConfig(geo, dir))
	assert.Nil(t, g.asnDB)
	assert.NotNil(t, g.countryDB)
	assert.Nil(t, g.cityIPv4DB)
	assert.Nil(t, g.cityIPv6DB)
}

// A custom server.geoip.dir is used verbatim, without the security/geoip
// suffix the default path adds.
func TestLoadFromConfig_UsesConfiguredDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, fileCountry), buildMinimalMMDB(t, 4), 0644))

	geo := enabledGeoIP(nil, nil)
	geo.Dir = dir

	g := &GeoIPDB{}
	require.NoError(t, g.LoadFromConfig(geo, "/nonexistent"))
	assert.True(t, g.HasCountryDB())
}

func TestDownloadFromConfig_DisabledIsNoOp(t *testing.T) {
	dir := t.TempDir()

	geo := enabledGeoIP(nil, nil)
	geo.Enabled = false

	require.NoError(t, DownloadFromConfig(geo, dir))

	_, err := os.Stat(geoipDir(dir))
	assert.True(t, os.IsNotExist(err))
}

func TestCategoryEnabled(t *testing.T) {
	dbs := config.GeoIPDatabasesConfig{ASN: true, Country: false, City: true}

	assert.True(t, categoryEnabled(dbs, categoryASN))
	assert.False(t, categoryEnabled(dbs, categoryCountry))
	assert.True(t, categoryEnabled(dbs, categoryCity))
	assert.False(t, categoryEnabled(dbs, "unknown"))
}

// Every configured MMDB file must map to a known category so the database
// selection config can gate it.
func TestGeoipFilesHaveKnownCategories(t *testing.T) {
	all := allDatabases()
	for _, f := range geoipFiles {
		assert.True(t, categoryEnabled(all, f.category), "file %s has unknown category %q", f.name, f.category)
	}
}

func TestCountryCodeOf_NilAndUnknown(t *testing.T) {
	g := loadedCountryDB(t)

	assert.Equal(t, "", g.CountryCodeOf(nil))
	assert.Equal(t, "", g.CountryCodeOf(net.ParseIP("8.8.8.8")))
}

// The MMDB file names and download URLs are fixed by AI.md PART 19: exactly
// four files, the country file carrying its upstream
// geo-whois-asn-country.mmdb name, and no MaxMind GeoLite2 source anywhere.
func TestGeoipFilesMatchSpec(t *testing.T) {
	want := map[string]string{
		"asn.mmdb":                   "https://cdn.jsdelivr.net/npm/@ip-location-db/asn-mmdb/asn.mmdb",
		"geo-whois-asn-country.mmdb": "https://cdn.jsdelivr.net/npm/@ip-location-db/geo-whois-asn-country-mmdb/geo-whois-asn-country.mmdb",
		"dbip-city-ipv4.mmdb":        "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv4.mmdb",
		"dbip-city-ipv6.mmdb":        "https://github.com/sapics/ip-location-db/releases/download/latest/dbip-city-ipv6.mmdb",
	}

	require.Len(t, geoipFiles, len(want))

	for _, f := range geoipFiles {
		url, known := want[f.name]
		require.True(t, known, "unexpected database file %q", f.name)
		assert.Equal(t, url, f.url)
		assert.NotContains(t, strings.ToLower(f.url), "geolite")
		assert.NotContains(t, strings.ToLower(f.url), "maxmind")
	}
}

// Country lists are ISO 3166-1 alpha-2 only: malformed entries are dropped,
// and a list that holds nothing else leaves the check with no rules to apply
// rather than blocking every request.
func TestCheckCountry_InvalidCodesAreDropped(t *testing.T) {
	g := loadedCountryDB(t)

	tests := []struct {
		name  string
		deny  []string
		allow []string
	}{
		{"deny list of only invalid codes", []string{"USA", "1A", "u"}, nil},
		{"allow list of only invalid codes", nil, []string{"united-states", ""}},
		{"both lists invalid", []string{"USA"}, []string{"CAN"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := g.CheckCountry("8.8.8.8", enabledGeoIP(tt.deny, tt.allow), nil)
			assert.False(t, decision.Blocked)
			assert.Equal(t, ReasonNoRules, decision.Reason)
		})
	}
}

// Mixed valid/invalid entries keep the valid ones and enforce on those alone.
func TestEvaluateCountryRules_DropsInvalidEntries(t *testing.T) {
	decision := evaluateCountryRules(
		"CN",
		nil,
		NormalizeCountryCodes([]string{"CHN", "cn", "1A"}),
	)
	assert.True(t, decision.Blocked)
	assert.Equal(t, ReasonCountryDenied, decision.Reason)
	assert.Equal(t, "CN", decision.CountryCode)
}

func TestPresetNames(t *testing.T) {
	geo := enabledGeoIP(nil, nil)
	geo.Presets = map[string][]string{
		"restricted": {"CN"},
		" audit ":    {"RU"},
		"   ":        {"US"},
	}

	assert.Equal(t, []string{" audit ", "restricted"}, PresetNames(geo))
	assert.Empty(t, PresetNames(enabledGeoIP(nil, nil)))
}

func TestValidatePresets(t *testing.T) {
	tests := []struct {
		name    string
		presets map[string][]string
		want    []string
	}{
		{"all valid", map[string][]string{"a": {"US", "ca"}}, nil},
		{"duplicates are not an error", map[string][]string{"a": {"US", "us"}}, nil},
		{"blank entries ignored", map[string][]string{"a": {"US", "  "}}, nil},
		{"malformed code reported", map[string][]string{"a": {"US", "USA"}}, []string{"a"}},
		{"empty preset reported", map[string][]string{"a": {}}, []string{"a"}},
		{"only invalid codes reported", map[string][]string{"a": {"1A"}}, []string{"a"}},
		{"unreachable whitespace key reported", map[string][]string{" a ": {"US"}}, []string{" a "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geo := enabledGeoIP(nil, nil)
			geo.Presets = tt.presets
			assert.Equal(t, tt.want, ValidatePresets(geo))
		})
	}
}

// An invalid preset is a warning, never a load failure - presets are never
// applied automatically, so they cannot affect enforcement either way.
func TestLoadFromConfig_InvalidPresetDoesNotFail(t *testing.T) {
	geo := enabledGeoIP(nil, nil)
	geo.Presets = map[string][]string{"broken": {"USA"}}

	g := &GeoIPDB{}
	require.NoError(t, g.LoadFromConfig(geo, t.TempDir()))
	assert.False(t, g.loaded)
}
