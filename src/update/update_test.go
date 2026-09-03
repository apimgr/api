package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidBranch(t *testing.T) {
	assert.True(t, ValidBranch(BranchStable))
	assert.True(t, ValidBranch(BranchBeta))
	assert.True(t, ValidBranch(BranchDaily))
	assert.False(t, ValidBranch("nightly"))
	assert.False(t, ValidBranch(""))
}

func TestSetBranch(t *testing.T) {
	cfg := &config.UpdateConfig{Branch: BranchStable}

	require.NoError(t, SetBranch(cfg, BranchBeta))
	assert.Equal(t, BranchBeta, cfg.Branch)

	err := SetBranch(cfg, "invalid")
	assert.Error(t, err)
	// An invalid branch must never overwrite the existing value.
	assert.Equal(t, BranchBeta, cfg.Branch)
}

func TestMatchesBranch(t *testing.T) {
	stable := Release{TagName: "1.2.3", Prerelease: false}
	beta := Release{TagName: "20260101000000-beta", Prerelease: true}
	daily := Release{TagName: "daily", Prerelease: true}

	// Stable releases match every channel.
	assert.True(t, matchesBranch(stable, BranchStable))
	assert.True(t, matchesBranch(stable, BranchBeta))
	assert.True(t, matchesBranch(stable, BranchDaily))

	// Beta releases match beta and daily, never stable.
	assert.False(t, matchesBranch(beta, BranchStable))
	assert.True(t, matchesBranch(beta, BranchBeta))
	assert.True(t, matchesBranch(beta, BranchDaily))

	// The rolling daily release matches only the daily channel.
	assert.False(t, matchesBranch(daily, BranchStable))
	assert.False(t, matchesBranch(daily, BranchBeta))
	assert.True(t, matchesBranch(daily, BranchDaily))
}

func TestBinaryAssetName(t *testing.T) {
	name := binaryAssetName()
	assert.Contains(t, name, "api-")
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	// Known SHA256 digest of the literal bytes "hello world".
	const expected = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	require.NoError(t, verifyChecksum(path, expected))
	assert.Error(t, verifyChecksum(path, "deadbeef"))
}

func TestFetchExpectedChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abc123  api-linux-amd64\ndef456  api-darwin-arm64\n"))
	}))
	defer srv.Close()

	release := &Release{
		Assets: []Asset{
			{Name: "sha256.txt", BrowserDownloadURL: srv.URL},
		},
	}

	hash, err := fetchExpectedChecksum(context.Background(), release, "api-linux-amd64")
	require.NoError(t, err)
	assert.Equal(t, "abc123", hash)

	_, err = fetchExpectedChecksum(context.Background(), release, "api-windows-amd64.exe")
	assert.Error(t, err)
}

func TestFetchExpectedChecksumMissingAsset(t *testing.T) {
	release := &Release{Assets: []Asset{}}
	_, err := fetchExpectedChecksum(context.Background(), release, "api-linux-amd64")
	assert.Error(t, err)
}

// withStubGitHubAPI points apiBaseURL at an httptest server for the
// duration of the test and restores it afterward.
func withStubGitHubAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	original := apiBaseURL
	apiBaseURL = srv.URL
	t.Cleanup(func() { apiBaseURL = original })
}

func TestCheckForUpdateStableAlreadyCurrent(t *testing.T) {
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/releases/latest")
		json.NewEncoder(w).Encode(Release{TagName: "1.2.3", Prerelease: false, PublishedAt: time.Now()})
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchStable, 0)
	require.NoError(t, err)
	assert.Nil(t, release)
}

func TestCheckForUpdateStableNewer(t *testing.T) {
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{TagName: "1.3.0", Prerelease: false, PublishedAt: time.Now()})
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchStable, 0)
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "1.3.0", release.TagName)
}

func TestCheckForUpdateStableNotFound(t *testing.T) {
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchStable, 0)
	require.NoError(t, err)
	assert.Nil(t, release)
}

func TestCheckForUpdateBetaSkipsOlderMatchesFirstNewer(t *testing.T) {
	now := time.Now()
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "20260201000000-beta", Prerelease: true, PublishedAt: now},
			{TagName: "1.2.3", Prerelease: false, PublishedAt: now.Add(-time.Hour)},
		})
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchBeta, 0)
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "20260201000000-beta", release.TagName)
}

func TestCheckForUpdateDailyNewerNightly(t *testing.T) {
	buildEpoch := time.Now().Add(-48 * time.Hour).Unix()
	publishedAt := time.Now().Add(-time.Hour)
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "daily", Prerelease: true, PublishedAt: publishedAt},
		})
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchDaily, buildEpoch)
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "daily", release.TagName)
}

func TestCheckForUpdateDailyStaleNightly(t *testing.T) {
	buildEpoch := time.Now().Unix()
	publishedAt := time.Now().Add(-48 * time.Hour)
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "daily", Prerelease: true, PublishedAt: publishedAt},
		})
	})

	release, err := CheckForUpdate(context.Background(), "1.2.3", BranchDaily, buildEpoch)
	require.NoError(t, err)
	assert.Nil(t, release)
}

func TestCheckEligibleRespectsDeferDays(t *testing.T) {
	now := time.Now()
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "1.2.5", Prerelease: false, PublishedAt: now.Add(-5 * 24 * time.Hour)},
			{TagName: "1.2.4", Prerelease: false, PublishedAt: now.Add(-40 * 24 * time.Hour)},
		})
	})

	// Under a 30 day defer window, only the older (1.2.4, published 40 days
	// ago) release is eligible; the newer 1.2.5 (5 days old) is skipped.
	release, err := CheckEligible(context.Background(), "1.2.3", BranchStable, 0, 30)
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "1.2.4", release.TagName)
}

func TestCheckEligibleZeroDeferAdoptsNewest(t *testing.T) {
	now := time.Now()
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "1.2.5", Prerelease: false, PublishedAt: now},
		})
	})

	release, err := CheckEligible(context.Background(), "1.2.3", BranchStable, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "1.2.5", release.TagName)
}

func TestCheckEligibleNoneEligible(t *testing.T) {
	now := time.Now()
	withStubGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Release{
			{TagName: "1.2.5", Prerelease: false, PublishedAt: now},
		})
	})

	release, err := CheckEligible(context.Background(), "1.2.3", BranchStable, 0, 30)
	require.NoError(t, err)
	assert.Nil(t, release)
}

func TestInstallNoMatchingAsset(t *testing.T) {
	release := &Release{
		TagName: "1.3.0",
		Assets:  []Asset{{Name: "some-other-binary", BrowserDownloadURL: "https://example.invalid/x"}},
	}

	err := Install(context.Background(), release)
	assert.Error(t, err)
}
