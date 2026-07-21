package research

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatCitationAPA(t *testing.T) {
	s := New()
	got := s.FormatCitationAPA("The Title", "Doe, J.", "2024", "Journal of Things")
	assert.Equal(t, "Doe, J.. (2024). The Title. Journal of Things", got)
}

func TestFormatCitationMLA(t *testing.T) {
	s := New()
	got := s.FormatCitationMLA("The Title", "Doe, J.", "Journal of Things", "2024")
	assert.Equal(t, `Doe, J.. "The Title." Journal of Things, 2024.`, got)
}

func TestFormatCitationChicago(t *testing.T) {
	s := New()
	got := s.FormatCitationChicago("Doe, J.", "The Title", "Journal of Things", "2024")
	assert.Equal(t, `Doe, J.. "The Title." Journal of Things (2024).`, got)
}

func TestGenerateBibliography(t *testing.T) {
	s := New()
	refs := []Reference{
		{Title: "Title One", Author: "Author One", Year: "2020", Source: "Source One"},
		{Title: "Title Two", Author: "Author Two", Year: "2021", Source: "Source Two"},
	}

	apa := s.GenerateBibliography(refs, "APA")
	assert.Len(t, apa, 2)
	assert.Equal(t, "Author One. (2020). Title One. Source One", apa[0])

	mla := s.GenerateBibliography(refs, "MLA")
	assert.Len(t, mla, 2)
	assert.Equal(t, `Author One. "Title One." Source One, 2020.`, mla[0])

	chicago := s.GenerateBibliography(refs, "Chicago")
	assert.Len(t, chicago, 2)
	assert.Equal(t, `Author One. "Title One." Source One (2020).`, chicago[0])

	// Unknown style falls back to a default format.
	def := s.GenerateBibliography(refs, "Unknown")
	assert.Len(t, def, 2)
	assert.Equal(t, "Author One - Title One (2020)", def[0])

	// Empty reference list yields an empty (nil) bibliography.
	empty := s.GenerateBibliography(nil, "APA")
	assert.Empty(t, empty)
}

func TestFormatDOI(t *testing.T) {
	s := New()
	assert.Equal(t, "https://doi.org/10.1000/xyz123", s.FormatDOI("10.1000/xyz123"))
}

func TestValidateDOI(t *testing.T) {
	s := New()
	assert.True(t, s.ValidateDOI("10.1000/xyz123"))
	assert.False(t, s.ValidateDOI("10.100"))
	assert.False(t, s.ValidateDOI("11.1000/xyz"))
	assert.False(t, s.ValidateDOI(""))
	assert.False(t, s.ValidateDOI("10"))
}
