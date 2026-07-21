package fun

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollDice(t *testing.T) {
	s := New()

	for i := 0; i < 50; i++ {
		roll, err := s.RollDice(6)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, roll, 1)
		assert.LessOrEqual(t, roll, 6)
	}

	_, err := s.RollDice(1)
	assert.Error(t, err)
	_, err = s.RollDice(0)
	assert.Error(t, err)
	_, err = s.RollDice(-5)
	assert.Error(t, err)
}

func TestRollMultipleDice(t *testing.T) {
	s := New()

	results, err := s.RollMultipleDice(5, 6)
	require.NoError(t, err)
	require.Len(t, results, 5)
	for _, r := range results {
		assert.GreaterOrEqual(t, r, 1)
		assert.LessOrEqual(t, r, 6)
	}

	_, err = s.RollMultipleDice(0, 6)
	assert.Error(t, err)
	_, err = s.RollMultipleDice(-1, 6)
	assert.Error(t, err)
}

func TestCoinFlip(t *testing.T) {
	s := New()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		result, err := s.CoinFlip()
		require.NoError(t, err)
		assert.Contains(t, []string{"heads", "tails"}, result)
		seen[result] = true
	}
}

func TestRandomChoice(t *testing.T) {
	s := New()

	choice, err := s.RandomChoice([]string{"only"})
	require.NoError(t, err)
	assert.Equal(t, "only", choice)

	choice, err = s.RandomChoice([]string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Contains(t, []string{"a", "b", "c"}, choice)

	_, err = s.RandomChoice(nil)
	assert.Error(t, err)
	_, err = s.RandomChoice([]string{})
	assert.Error(t, err)
}

func TestMagic8BallFortuneYesOrNoEmojiJokeType(t *testing.T) {
	s := New()

	answer, err := s.Magic8Ball()
	require.NoError(t, err)
	assert.NotEmpty(t, answer)

	fortune, err := s.Fortune()
	require.NoError(t, err)
	assert.NotEmpty(t, fortune)

	yn, err := s.YesOrNo()
	require.NoError(t, err)
	assert.Contains(t, []string{"yes", "no"}, yn)

	emoji, err := s.RandomEmoji()
	require.NoError(t, err)
	assert.NotEmpty(t, emoji)

	jokeType, err := s.RandomJokeType()
	require.NoError(t, err)
	assert.NotEmpty(t, jokeType)
}

func TestShuffle(t *testing.T) {
	s := New()

	original := []string{"a", "b", "c", "d", "e"}
	shuffled, err := s.Shuffle(original)
	require.NoError(t, err)

	// Shuffle must not mutate the input slice.
	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, original)

	// Result must be a permutation containing exactly the same elements.
	sortedOriginal := append([]string(nil), original...)
	sortedShuffled := append([]string(nil), shuffled...)
	sort.Strings(sortedOriginal)
	sort.Strings(sortedShuffled)
	assert.Equal(t, sortedOriginal, sortedShuffled)

	// Empty slice shuffles to empty slice without error.
	empty, err := s.Shuffle([]string{})
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Single element slice shuffles to itself.
	single, err := s.Shuffle([]string{"only"})
	require.NoError(t, err)
	assert.Equal(t, []string{"only"}, single)
}

func TestRockPaperScissors(t *testing.T) {
	s := New()

	result, err := s.RockPaperScissors("rock")
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Case-insensitive and trims whitespace.
	result, err = s.RockPaperScissors("  ROCK  ")
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	_, err = s.RockPaperScissors("lizard")
	assert.Error(t, err)
	_, err = s.RockPaperScissors("")
	assert.Error(t, err)
}
