package scheduler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func trivyLayer(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	for name, body := range files {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestInstallTrivyDatabaseExtractsAndReplaces(t *testing.T) {
	target := filepath.Join(t.TempDir(), "trivy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "trivy.db"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(fmt.Sprintf(`{"Version":2,"NextUpdate":%q,"UpdatedAt":%q}`, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)))
	payload := trivyLayer(t, map[string][]byte{"db/trivy.db": []byte("new database"), "db/metadata.json": metadata, "../../outside": []byte("ignored")})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	if err := installTrivyDatabase(context.Background(), server.Client(), server.URL, "token", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "trivy.db"))
	if err != nil || string(got) != "new database" {
		t.Fatalf("trivy.db=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(target, "metadata.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, ".last_updated")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), "outside")); !os.IsNotExist(err) {
		t.Fatalf("unexpected traversal output: %v", err)
	}
}

func TestInstallTrivyDatabaseRejectsInvalidMetadataAndKeepsPrevious(t *testing.T) {
	target := filepath.Join(t.TempDir(), "trivy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "trivy.db"), []byte("last good"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := trivyLayer(t, map[string][]byte{"trivy.db": []byte("bad"), "metadata.json": []byte(`{"Version":0}`)})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	if err := installTrivyDatabase(context.Background(), server.Client(), server.URL, "token", target); err == nil {
		t.Fatal("expected invalid metadata error")
	}
	got, err := os.ReadFile(filepath.Join(target, "trivy.db"))
	if err != nil || string(got) != "last good" {
		t.Fatalf("previous database changed: %q err=%v", got, err)
	}
}

func TestTrivyDatabaseFreshSkipsUntilNextUpdate(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "trivy.db"), []byte("database"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := fmt.Sprintf(`{"Version":2,"NextUpdate":%q}`, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(directory, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	if !trivyDatabaseFresh(directory, time.Now().UTC()) {
		t.Fatal("expected fresh database")
	}
	if trivyDatabaseFresh(directory, time.Now().UTC().Add(2*time.Hour)) {
		t.Fatal("expired database reported fresh")
	}
}

func TestTrivyLayerDigestSelectsRequiredMediaType(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.oci.image.manifest.v1+json" {
			t.Errorf("unexpected Accept: %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"layers":[{"digest":"sha256:wrong","mediaType":"application/vnd.oci.image.layer.v1.tar+gzip"},{"digest":"sha256:right","mediaType":"application/vnd.aquasec.trivy.db.layer.v1.tar+gzip"}]}`))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := trivyLayerDigestWithClient(context.Background(), server.Client(), "token", parsed.Host, "repo/db", "2")
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:right" {
		t.Fatalf("digest=%q", digest)
	}
}
