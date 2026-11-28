package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultEmojiEnabled(t *testing.T) {
	tests := []struct {
		name       string
		terminal   bool
		noColor    string
		term       string
		wantEnable bool
	}{
		{"normal terminal", true, "", "xterm-256color", true},
		{"non terminal", false, "", "xterm-256color", false},
		{"no color", true, "1", "xterm-256color", false},
		{"dumb terminal", true, "", "dumb", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.wantEnable, emojiOutputEnabled(test.terminal, test.noColor, test.term))
		})
	}
}

func TestApplyEmojiOverrideRespectsDumbTerminal(t *testing.T) {
	configEmoji := true
	t.Setenv("TERM", "dumb")
	emojiEnabled = false
	applyEmojiOverride(&configEmoji)
	assert.False(t, emojiEnabled)
}
