package generate

import (
	"bytes"
	"encoding/json"
	"image/png"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBarcode(t *testing.T) {
	s := New()

	cases := []struct {
		name   string
		format string
		data   string
	}{
		{"code128", "code128", "Hello123"},
		{"code39", "code39", "HELLO"},
		{"ean13", "ean13", "590123412345"},
		{"upca", "upca", "03600029145"},
	}

	for _, c := range cases {
		png, err := s.Barcode(c.format, c.data, 300, 100)
		require.NoError(t, err, c.name)
		assertValidPNG(t, png, 300, 100)
	}
}

func TestBarcodeDefaults(t *testing.T) {
	s := New()

	png, err := s.Barcode("code128", "abc", 0, 0)
	require.NoError(t, err)
	assertValidPNG(t, png, 300, 100)
}

func TestBarcodeUnsupportedFormat(t *testing.T) {
	s := New()

	_, err := s.Barcode("nope", "abc", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported barcode format")
}

func TestBarcodeUPCAInvalidLength(t *testing.T) {
	s := New()

	_, err := s.Barcode("upca", "1234", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upca data must be")
}

func TestAvatar(t *testing.T) {
	s := New()

	png, err := s.Avatar("AB", 128)
	require.NoError(t, err)
	assertValidPNG(t, png, 128, 128)
}

func TestAvatarDefaultSize(t *testing.T) {
	s := New()

	png, err := s.Avatar("XY", 0)
	require.NoError(t, err)
	assertValidPNG(t, png, 256, 256)
}

func TestAvatarDeterministic(t *testing.T) {
	s := New()

	a, err := s.Avatar("AB", 64)
	require.NoError(t, err)
	b, err := s.Avatar("AB", 64)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestAvatarEmptyInitials(t *testing.T) {
	s := New()

	_, err := s.Avatar("", 64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initials must not be empty")
}

func TestIdenticon(t *testing.T) {
	s := New()

	png, err := s.Identicon("user@example.com", 256)
	require.NoError(t, err)
	assertValidPNG(t, png, 256, 256)
}

func TestIdenticonDefaultSize(t *testing.T) {
	s := New()

	png, err := s.Identicon("seed", 0)
	require.NoError(t, err)
	assertValidPNG(t, png, 256, 256)
}

func TestIdenticonDeterministic(t *testing.T) {
	s := New()

	a, err := s.Identicon("seed", 64)
	require.NoError(t, err)
	b, err := s.Identicon("seed", 64)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestIdenticonEmptySeed(t *testing.T) {
	s := New()

	_, err := s.Identicon("", 64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seed must not be empty")
}

func TestDockerfile(t *testing.T) {
	s := New()

	cases := []struct {
		lang     string
		contains string
	}{
		{"go", "FROM golang"},
		{"node", "FROM node"},
		{"python", "FROM python"},
		{"rust", "FROM rust"},
		{"generic", "FROM alpine"},
		{"", "FROM alpine"},
	}

	for _, c := range cases {
		out, err := s.Dockerfile(c.lang)
		require.NoError(t, err, c.lang)
		assert.Contains(t, out, c.contains, c.lang)
	}
}

func TestDockerfileUnsupportedLang(t *testing.T) {
	s := New()

	_, err := s.Dockerfile("cobol")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported language")
}

func TestGitignore(t *testing.T) {
	s := New()

	out, err := s.Gitignore("go,vscode,macos")
	require.NoError(t, err)
	assert.Contains(t, out, "# Go")
	assert.Contains(t, out, "# VSCode")
	assert.Contains(t, out, "# macOS")
}

func TestGitignoreEmpty(t *testing.T) {
	s := New()

	_, err := s.Gitignore("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one language")
}

func TestGitignoreUnknownLang(t *testing.T) {
	s := New()

	_, err := s.Gitignore("go,cobol")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported language(s)")
}

func TestLicenseMIT(t *testing.T) {
	s := New()

	out, err := s.License("mit", "Jane Doe", "2026")
	require.NoError(t, err)
	assert.Contains(t, out, "MIT License")
	assert.Contains(t, out, "Copyright (c) 2026 Jane Doe")
}

func TestLicenseDefaults(t *testing.T) {
	s := New()

	out, err := s.License("mit", "", "")
	require.NoError(t, err)
	assert.Contains(t, out, "The Authors")
}

func TestLicenseApache2(t *testing.T) {
	s := New()

	out, err := s.License("apache-2.0", "", "")
	require.NoError(t, err)
	assert.Contains(t, out, "Apache License")
	assert.Contains(t, out, "Version 2.0")
}

func TestLicenseGPL3(t *testing.T) {
	s := New()

	out, err := s.License("gpl-3.0", "", "")
	require.NoError(t, err)
	assert.Contains(t, out, "GNU GENERAL PUBLIC LICENSE")
	assert.Contains(t, out, "Version 3")
}

func TestLicenseBSD3(t *testing.T) {
	s := New()

	out, err := s.License("bsd-3-clause", "Jane Doe", "2026")
	require.NoError(t, err)
	assert.Contains(t, out, "BSD 3-Clause License")
	assert.Contains(t, out, "Copyright (c) 2026, Jane Doe")
}

func TestLicenseISC(t *testing.T) {
	s := New()

	out, err := s.License("isc", "Jane Doe", "2026")
	require.NoError(t, err)
	assert.Contains(t, out, "ISC License")
	assert.Contains(t, out, "Copyright (c) 2026, Jane Doe")
}

func TestLicenseUnsupported(t *testing.T) {
	s := New()

	_, err := s.License("wtfpl", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported license type")
}

func TestConfigYAML(t *testing.T) {
	s := New()

	out, err := s.Config("yaml", map[string]string{"host": "localhost", "port": "8080"})
	require.NoError(t, err)
	assert.Contains(t, out, "host: localhost\n")
	assert.Contains(t, out, "port: 8080\n")
}

func TestConfigJSON(t *testing.T) {
	s := New()

	out, err := s.Config("json", map[string]string{"host": "localhost"})
	require.NoError(t, err)
	assert.Contains(t, out, `"host": "localhost"`)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, "localhost", decoded["host"])
}

func TestConfigEnv(t *testing.T) {
	s := New()

	out, err := s.Config("env", map[string]string{"db-host": "localhost"})
	require.NoError(t, err)
	assert.Contains(t, out, "DB_HOST=localhost\n")
}

func TestConfigTOML(t *testing.T) {
	s := New()

	out, err := s.Config("toml", map[string]string{"host": "localhost", "port": "8080"})
	require.NoError(t, err)
	assert.Contains(t, out, `host = "localhost"`)
	assert.Contains(t, out, "port = 8080")
}

func TestConfigEmptyValues(t *testing.T) {
	s := New()

	_, err := s.Config("yaml", map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key=value pair")
}

func TestConfigUnsupportedFormat(t *testing.T) {
	s := New()

	_, err := s.Config("ini", map[string]string{"a": "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config format")
}

func TestSQL(t *testing.T) {
	s := New()

	out, err := s.SQL("users", []SQLColumn{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "email", Type: "string", Unique: true},
	})
	require.NoError(t, err)
	assert.Contains(t, out, "CREATE TABLE users (")
	assert.Contains(t, out, "id INTEGER NOT NULL")
	assert.Contains(t, out, "email VARCHAR(255) UNIQUE")
	assert.Contains(t, out, "PRIMARY KEY (id)")
}

func TestSQLMissingTable(t *testing.T) {
	s := New()

	_, err := s.SQL("", []SQLColumn{{Name: "id", Type: "integer"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table name is required")
}

func TestSQLNoColumns(t *testing.T) {
	s := New()

	_, err := s.SQL("users", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one column is required")
}

func TestSQLEmptyColumnName(t *testing.T) {
	s := New()

	_, err := s.SQL("users", []SQLColumn{{Name: "", Type: "integer"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "column name must not be empty")
}

func TestSQLUnsupportedColumnType(t *testing.T) {
	s := New()

	_, err := s.SQL("users", []SQLColumn{{Name: "id", Type: "blob"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported column type")
}

func TestSSHKey(t *testing.T) {
	s := New()

	pair, err := s.SSHKey()
	require.NoError(t, err)
	assert.Contains(t, pair.PrivateKey, "BEGIN OPENSSH PRIVATE KEY")
	assert.Contains(t, pair.PublicKey, "ssh-ed25519")

	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pair.PublicKey))
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", pub.Type())
}

func TestSSHKeyUnique(t *testing.T) {
	s := New()

	a, err := s.SSHKey()
	require.NoError(t, err)
	b, err := s.SSHKey()
	require.NoError(t, err)
	assert.NotEqual(t, a.PublicKey, b.PublicKey)
}

func TestAPIDocsMarkdown(t *testing.T) {
	s := New()

	out, err := s.APIDocs("markdown", "v1", "https://example.com")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "# "))
	assert.Contains(t, out, "Version: v1")
}

func TestAPIDocsJSON(t *testing.T) {
	s := New()

	out, err := s.APIDocs("json", "v1", "https://example.com")
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Contains(t, decoded, "paths")
}

func TestAPIDocsDefaultFormat(t *testing.T) {
	s := New()

	out, err := s.APIDocs("", "v1", "https://example.com")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "# "))
}

// assertValidPNG decodes b as a PNG and asserts its dimensions match
// wantWidth x wantHeight.
func assertValidPNG(t *testing.T, b []byte, wantWidth, wantHeight int) {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	bounds := img.Bounds()
	assert.Equal(t, wantWidth, bounds.Dx(), "width mismatch (got "+strconv.Itoa(bounds.Dx())+")")
	assert.Equal(t, wantHeight, bounds.Dy(), "height mismatch (got "+strconv.Itoa(bounds.Dy())+")")
}
