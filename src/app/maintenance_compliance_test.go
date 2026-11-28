package app

import (
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOwnerCommandArgs(t *testing.T) {
	prefix, output, yes, err := parseOwnerCommandArgs([]string{"--owner", "own_1234567890", "--output", "-"}, true, false)
	require.NoError(t, err)
	assert.Equal(t, "own_1234567890", prefix)
	assert.Equal(t, "-", output)
	assert.False(t, yes)

	prefix, _, yes, err = parseOwnerCommandArgs([]string{"--yes", "--owner", "own_1234567890"}, false, true)
	require.NoError(t, err)
	assert.Equal(t, "own_1234567890", prefix)
	assert.True(t, yes)
}

func TestParseOwnerCommandArgsRejectsDuplicateAndUnpairedOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--owner", "a", "--owner", "b"},
		{"--owner"},
		{"--owner", "a", "--output", "file"},
		{"--owner", "a", "--yes"},
		{"--unknown"},
	} {
		_, _, _, err := parseOwnerCommandArgs(args, false, false)
		assert.Error(t, err, args)
	}
}

func TestComplianceSettingsSummaryListsEachIndependentStandard(t *testing.T) {
	got := complianceSettingsSummary(config.ComplianceConfig{GDPR: true, HIPAA: true})
	assert.Contains(t, got, "gdpr=true")
	assert.Contains(t, got, "hipaa=true")
	assert.Contains(t, got, "ccpa=false")
	assert.Equal(t, "none", formatRetentionDays(0))
	assert.Equal(t, "365d", formatRetentionDays(365))
}
