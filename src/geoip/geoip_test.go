package geoip

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, "asn.mmdb"), []byte("not a real mmdb"), 0644))

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

	require.NoError(t, os.WriteFile(filepath.Join(geoDir, "asn.mmdb"), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, "country.mmdb"), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, "dbip-city-ipv4.mmdb"), ipv4, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(geoDir, "dbip-city-ipv6.mmdb"), ipv6, 0644))

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
