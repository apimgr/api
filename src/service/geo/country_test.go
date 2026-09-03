package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers Country: empty query rejected, valid alpha-2/alpha-3/full-name
// lookups resolve consistently, and an unknown country is rejected.
func TestCountry(t *testing.T) {
	s := New()

	t.Run("empty query", func(t *testing.T) {
		info, err := s.Country("")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "query is required")
	})

	t.Run("alpha-2 lookup", func(t *testing.T) {
		info, err := s.Country("US")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "US", info.Alpha2)
		assert.Equal(t, "USA", info.Alpha3)
		assert.NotEmpty(t, info.Capital)
		assert.NotEmpty(t, info.Currency)
		assert.NotEmpty(t, info.TLD)
		assert.NotEmpty(t, info.Region)
	})

	t.Run("alpha-3 and full name resolve the same country", func(t *testing.T) {
		byAlpha3, err := s.Country("USA")
		require.NoError(t, err)
		byName, err := s.Country("United States")
		require.NoError(t, err)
		assert.Equal(t, byAlpha3.Alpha2, byName.Alpha2)
	})

	t.Run("unknown country", func(t *testing.T) {
		info, err := s.Country("Not A Real Country")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "country not found")
	})
}
