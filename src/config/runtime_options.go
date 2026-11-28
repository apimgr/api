package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/paths"
)

// RuntimeOptions contains command-line selections that are not persisted in
// the server configuration file.
type RuntimeOptions struct {
	RawArgs     []string
	Args        []string
	Help        bool
	Version     bool
	Mode        string
	ConfigDir   string
	DataDir     string
	LogDir      string
	CacheDir    string
	BackupDir   string
	PIDFile     string
	Address     string
	Port        string
	BaseURL     string
	I2P         bool
	I2PSet      bool
	Daemon      bool
	Debug       bool
	DebugSet    bool
	Color       string
	Language    string
	Shell       string
	CommandHelp string
	Status      bool
	Service     string
	Maintenance string
	Update      string
}

// RuntimeArgumentError identifies invalid command-line arguments.
type RuntimeArgumentError struct {
	Err error
}

func (e *RuntimeArgumentError) Error() string { return e.Err.Error() }

func (e *RuntimeArgumentError) Unwrap() error { return e.Err }

// ParseRuntimeOptions parses the server command line using the standard flag
// package so all argument parsing stays in the configuration layer.
func ParseRuntimeOptions(arguments []string) (RuntimeOptions, error) {
	var options RuntimeOptions
	options.RawArgs = append([]string(nil), arguments...)
	if command := commandFlagHelp(arguments, "shell", "service", "maintenance", "update"); command != "" {
		options.CommandHelp = command
		return options, nil
	}
	arguments = append([]string(nil), arguments...)
	for index, argument := range arguments {
		if argument == "--update" && (index+1 == len(arguments) || strings.HasPrefix(arguments[index+1], "-")) {
			arguments[index] = "--update=yes"
		}
	}
	set := flag.NewFlagSet("api", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&options.Help, "help", false, i18n.Translate(i18n.DefaultLanguage, "cli.show_help"))
	set.BoolVar(&options.Help, "h", false, i18n.Translate(i18n.DefaultLanguage, "cli.show_help"))
	set.BoolVar(&options.Version, "version", false, i18n.Translate(i18n.DefaultLanguage, "cli.show_version"))
	set.BoolVar(&options.Version, "v", false, i18n.Translate(i18n.DefaultLanguage, "cli.show_version"))
	set.StringVar(&options.Mode, "mode", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_mode"))
	set.StringVar(&options.ConfigDir, "config", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_config"))
	set.StringVar(&options.DataDir, "data", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_data"))
	set.StringVar(&options.LogDir, "log", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_log"))
	set.StringVar(&options.CacheDir, "cache", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_cache"))
	set.StringVar(&options.BackupDir, "backup", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_backup"))
	set.StringVar(&options.PIDFile, "pid", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_pid"))
	set.StringVar(&options.Address, "address", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_address"))
	set.StringVar(&options.Port, "port", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_port"))
	set.StringVar(&options.BaseURL, "baseurl", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_baseurl"))
	set.BoolVar(&options.I2P, "i2p", false, i18n.Translate(i18n.DefaultLanguage, "cli.flag_i2p"))
	set.BoolVar(&options.Daemon, "daemon", false, i18n.Translate(i18n.DefaultLanguage, "cli.flag_daemon"))
	set.BoolVar(&options.Debug, "debug", false, i18n.Translate(i18n.DefaultLanguage, "cli.flag_debug"))
	set.StringVar(&options.Color, "color", "auto", i18n.Translate(i18n.DefaultLanguage, "cli.flag_color"))
	set.StringVar(&options.Language, "lang", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_lang"))
	set.StringVar(&options.Shell, "shell", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_shell"))
	set.BoolVar(&options.Status, "status", false, i18n.Translate(i18n.DefaultLanguage, "cli.flag_status"))
	set.StringVar(&options.Service, "service", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_service"))
	set.StringVar(&options.Maintenance, "maintenance", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_maintenance"))
	set.StringVar(&options.Update, "update", "", i18n.Translate(i18n.DefaultLanguage, "cli.flag_update"))
	if err := set.Parse(arguments); err != nil && !errors.Is(err, flag.ErrHelp) {
		return options, &RuntimeArgumentError{Err: err}
	}
	options.Args = append([]string(nil), set.Args()...)
	set.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "i2p":
			options.I2PSet = true
		case "debug":
			options.DebugSet = true
		}
	})
	return options, nil
}

func commandFlagHelp(args []string, commands ...string) string {
	for index := 0; index+2 < len(args); index++ {
		if args[index] == "--maintenance" && strings.EqualFold(args[index+1], "tor") && isHelpArgument(args[index+2]) {
			return "tor"
		}
	}
	for index := 0; index+1 < len(args); index++ {
		if !isHelpArgument(args[index+1]) {
			continue
		}
		for _, command := range commands {
			if args[index] == "--"+command {
				return command
			}
		}
	}
	return ""
}

func isHelpArgument(argument string) bool {
	return argument == "--help" || argument == "-h" || strings.EqualFold(argument, "help")
}

func initializeRuntimePaths(options *RuntimeOptions) error {
	configDir := runtimePathOption(options.ConfigDir, "CONFIG_DIR")
	dataDir := runtimePathOption(options.DataDir, "DATA_DIR")
	logDir := runtimePathOption(options.LogDir, "LOG_DIR")
	cacheDir := runtimePathOption(options.CacheDir, "CACHE_DIR")
	backupDir := runtimePathOption(options.BackupDir, "BACKUP_DIR")
	pidFile := runtimePathOption(options.PIDFile, "PID_FILE")
	for _, directory := range []*string{&configDir, &dataDir, &logDir, &cacheDir, &backupDir} {
		if *directory == "" {
			continue
		}
		safe, err := paths.SafeDirPath(*directory)
		if err != nil {
			return fmt.Errorf("invalid directory path %q: %w", *directory, err)
		}
		*directory = safe
	}
	if pidFile != "" {
		safe, err := paths.SafeDirPath(pidFile)
		if err != nil {
			return fmt.Errorf("invalid PID file path %q: %w", pidFile, err)
		}
		pidFile = safe
	}
	options.ConfigDir = configDir
	options.DataDir = dataDir
	options.LogDir = logDir
	options.CacheDir = cacheDir
	options.BackupDir = backupDir
	options.PIDFile = pidFile
	options.Address = runtimePathOption(options.Address, "LISTEN")
	options.Port = runtimePathOption(options.Port, "PORT")
	paths.SetApplicationPathOverrides(configDir, dataDir, logDir)
	paths.InitCache(cacheDir)
	paths.InitBackup(backupDir)
	return nil
}

func runtimePathOption(flagValue, envKey string) string {
	if flagValue != "" {
		return flagValue
	}
	return strings.TrimSpace(os.Getenv(envKey))
}

func isEmailCommand(arguments []string) bool {
	return len(arguments) > 0 && strings.EqualFold(arguments[0], "email")
}

func applyRuntimeOptions(cfg *ServerConfig) {
	options := cfg.Runtime
	if options.Address != "" {
		cfg.Server.Address = options.Address
	}
	if options.Port != "" {
		cfg.Server.Port = options.Port
	}
	if options.BaseURL != "" {
		cfg.Server.BaseURL = options.BaseURL
	}
	if options.I2PSet {
		cfg.Server.I2P.Enabled = options.I2P
	}
}
