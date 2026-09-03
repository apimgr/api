package language

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLanguageName(t *testing.T) {
	s := New()

	name, err := s.GetLanguageName("en")
	require.NoError(t, err)
	assert.Equal(t, "English", name)

	// Lookup is case-insensitive.
	name, err = s.GetLanguageName("FR")
	require.NoError(t, err)
	assert.Equal(t, "French", name)

	_, err = s.GetLanguageName("xx")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "xx")

	_, err = s.GetLanguageName("")
	assert.Error(t, err)
}

func TestListLanguages(t *testing.T) {
	s := New()

	languages := s.ListLanguages()
	assert.Len(t, languages, 12)
	assert.Equal(t, "English", languages["en"])
	assert.Equal(t, "Spanish", languages["es"])
	assert.Equal(t, "Chinese", languages["zh"])
}
