package geoip

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
