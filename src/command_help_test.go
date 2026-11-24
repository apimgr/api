package main

import "testing"

func TestCommandFlagHelp(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"long help", []string{"--shell", "--help"}, "shell"},
		{"short help", []string{"--service", "-h"}, "service"},
		{"help command", []string{"--maintenance", "help"}, "maintenance"},
		{"tor help", []string{"--tor", "--help"}, "tor"},
		{"global help", []string{"--help"}, ""},
		{"ordinary value", []string{"--shell", "completions", "--help"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := commandFlagHelp(test.args, "shell", "service", "maintenance", "update", "tor"); got != test.want {
				t.Fatalf("commandFlagHelp(%v) = %q, want %q", test.args, got, test.want)
			}
		})
	}
}
