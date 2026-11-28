package config

import (
	"errors"
	"testing"
)

func TestParseRuntimeOptions(t *testing.T) {
	options, err := ParseRuntimeOptions([]string{
		"--mode=debug", "--address", "127.0.0.1", "--debug", "--i2p", "email", "test",
	})
	if err != nil {
		t.Fatalf("ParseRuntimeOptions() error = %v", err)
	}
	if options.Mode != "debug" || options.Address != "127.0.0.1" {
		t.Fatalf("ParseRuntimeOptions() options = %#v", options)
	}
	if !options.Debug || !options.DebugSet || !options.I2P || !options.I2PSet {
		t.Fatalf("explicit boolean flags were not recorded: %#v", options)
	}
	if len(options.Args) != 2 || options.Args[0] != "email" || options.Args[1] != "test" {
		t.Fatalf("ParseRuntimeOptions() args = %v", options.Args)
	}
}

func TestParseRuntimeOptionsInvalidFlag(t *testing.T) {
	_, err := ParseRuntimeOptions([]string{"--unknown"})
	var argumentError *RuntimeArgumentError
	if !errors.As(err, &argumentError) {
		t.Fatalf("ParseRuntimeOptions() error = %v, want RuntimeArgumentError", err)
	}
}

func TestParseRuntimeOptionsBareUpdateDefaultsToYes(t *testing.T) {
	options, err := ParseRuntimeOptions([]string{"--update"})
	if err != nil {
		t.Fatalf("ParseRuntimeOptions() error = %v", err)
	}
	if options.Update != "yes" {
		t.Fatalf("ParseRuntimeOptions() update = %q, want yes", options.Update)
	}
}

func TestParseRuntimeOptionsCommandHelpPreflight(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "shell long help", args: []string{"--shell", "--help"}, want: "shell"},
		{name: "service short help", args: []string{"--service", "-h"}, want: "service"},
		{name: "maintenance help word", args: []string{"--maintenance", "help"}, want: "maintenance"},
		{name: "update long help", args: []string{"--update", "--help"}, want: "update"},
		{name: "nested tor help", args: []string{"--maintenance", "tor", "--help"}, want: "tor"},
		{name: "ordinary command value", args: []string{"--shell", "completions", "--help"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, err := ParseRuntimeOptions(test.args)
			if err != nil {
				t.Fatalf("ParseRuntimeOptions() error = %v", err)
			}
			if options.CommandHelp != test.want {
				t.Fatalf("ParseRuntimeOptions() CommandHelp = %q, want %q", options.CommandHelp, test.want)
			}
		})
	}
}

func TestLoadCommandHelpSkipsPathAndConfigLoading(t *testing.T) {
	t.Setenv("CONFIG_DIR", "../invalid-config-dir")

	options, err := Load([]string{"--shell", "--help"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if options.Runtime.CommandHelp != "shell" {
		t.Fatalf("Load() CommandHelp = %q, want shell", options.Runtime.CommandHelp)
	}
}

func TestParseRuntimeOptionsRoutesTorUnderMaintenance(t *testing.T) {
	options, err := ParseRuntimeOptions([]string{"--maintenance", "tor", "status"})
	if err != nil {
		t.Fatalf("ParseRuntimeOptions() error = %v", err)
	}
	if options.Maintenance != "tor" || len(options.Args) != 1 || options.Args[0] != "status" {
		t.Fatalf("ParseRuntimeOptions() options = %#v", options)
	}
	if _, err := ParseRuntimeOptions([]string{"--tor", "status"}); err == nil {
		t.Fatal("ParseRuntimeOptions(--tor) succeeded; top-level --tor is not supported")
	}
}
