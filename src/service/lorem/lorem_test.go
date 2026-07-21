package lorem

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWords(t *testing.T) {
	s := New()

	words, err := s.Words(5)
	require.NoError(t, err)
	assert.Len(t, strings.Fields(words), 5)

	// count < 1 is clamped up to 1 word.
	words, err = s.Words(0)
	require.NoError(t, err)
	assert.Len(t, strings.Fields(words), 1)

	words, err = s.Words(-3)
	require.NoError(t, err)
	assert.Len(t, strings.Fields(words), 1)
}

func TestSentence(t *testing.T) {
	s := New()

	sentence, err := s.Sentence(5)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(sentence, "."))
	// First letter capitalized.
	first := rune(sentence[0])
	assert.True(t, unicode.IsUpper(first))
	// 5 words plus trailing period.
	wordCount := len(strings.Fields(strings.TrimSuffix(sentence, ".")))
	assert.Equal(t, 5, wordCount)

	// wordCount < 1 defaults to 10 words.
	sentence, err = s.Sentence(0)
	require.NoError(t, err)
	wordCount = len(strings.Fields(strings.TrimSuffix(sentence, ".")))
	assert.Equal(t, 10, wordCount)
}

func TestParagraph(t *testing.T) {
	s := New()

	paragraph, err := s.Paragraph(3)
	require.NoError(t, err)
	// Sentences are period-terminated and joined with spaces; count periods.
	periodCount := strings.Count(paragraph, ".")
	assert.Equal(t, 3, periodCount)

	// sentenceCount < 1 defaults to 5 sentences.
	paragraph, err = s.Paragraph(0)
	require.NoError(t, err)
	assert.Equal(t, 5, strings.Count(paragraph, "."))
}

func TestPerson(t *testing.T) {
	s := New()

	person, err := s.Person()
	require.NoError(t, err)
	require.Contains(t, person, "name")
	require.Contains(t, person, "email")
	require.Contains(t, person, "phone")

	assert.NotEmpty(t, person["name"])
	assert.Regexp(t, regexp.MustCompile(`^[a-z]+\.[a-z]+@example\.com$`), person["email"])
	assert.Regexp(t, regexp.MustCompile(`^\+1-555-\d{4}$`), person["phone"])
}

func TestAddress(t *testing.T) {
	s := New()

	address, err := s.Address()
	require.NoError(t, err)
	require.Contains(t, address, "street")
	require.Contains(t, address, "city")
	require.Contains(t, address, "state")
	require.Contains(t, address, "zip")

	assert.Regexp(t, regexp.MustCompile(`^\d+ Main St$`), address["street"])
	assert.NotEmpty(t, address["city"])
	assert.NotEmpty(t, address["state"])
	assert.Regexp(t, regexp.MustCompile(`^\d{5}$`), address["zip"])
}

func TestCompany(t *testing.T) {
	s := New()

	company, err := s.Company()
	require.NoError(t, err)
	require.Contains(t, company, "name")
	require.Contains(t, company, "industry")
	assert.NotEmpty(t, company["name"])
	assert.NotEmpty(t, company["industry"])
}
