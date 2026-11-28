package app

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/sysservice"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestExitCodeUsesTwoForUsageErrors(t *testing.T) {
	err := &ExitError{Code: exUsage, Err: errors.New("invalid argument")}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2 for a usage error", got)
	}
}

func TestStartupURLsOmitUnresolvedHost(t *testing.T) {
	got := buildStartupURLs("8080", "", func(string, bool) string { return "" })
	if len(got) != 0 {
		t.Fatalf("buildStartupURLs() = %q, want no URLs when hostname is unresolved", got)
	}
}

func TestShouldDaemonizeManuallyOnlyWithoutServiceManager(t *testing.T) {
	for _, test := range []struct {
		name      string
		requested bool
		container bool
		manager   sysservice.ServiceType
		service   bool
		want      bool
	}{
		{name: "manual request", requested: true, manager: sysservice.ServiceUnknown, want: runtime.GOOS != "windows"},
		{name: "systemd service manager", requested: true, manager: sysservice.ServiceSystemd},
		{name: "OpenRC service manager", requested: true, manager: sysservice.ServiceOpenRC},
		{name: "runit service manager", requested: true, manager: sysservice.ServiceRunit},
		{name: "s6 service manager", requested: true, manager: sysservice.ServiceS6},
		{name: "launchd service manager", requested: true, manager: sysservice.ServiceLaunchd},
		{name: "BSD rc service manager", requested: true, manager: sysservice.ServiceBSDRC},
		{name: "SysV service manager", requested: true, manager: sysservice.ServiceSysVinit},
		{name: "container", requested: true, container: true, manager: sysservice.ServiceUnknown},
		{name: "service invocation", requested: true, manager: sysservice.ServiceUnknown, service: true},
		{name: "not requested", manager: sysservice.ServiceUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldDaemonizeManually(test.requested, test.container, test.manager, test.service); got != test.want {
				t.Fatalf("shouldDaemonizeManually() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestIsServiceManagerContext(t *testing.T) {
	for _, test := range []struct {
		name         string
		parentPID    int
		invocationID string
		rcService    string
		svDir        string
		s6Logging    string
		want         bool
	}{
		{name: "ordinary manual process"},
		{name: "init parent", parentPID: 1, want: true},
		{name: "systemd", invocationID: "unit", want: true},
		{name: "OpenRC", rcService: "api", want: true},
		{name: "runit", svDir: "/run/service", want: true},
		{name: "s6", s6Logging: "1", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := isServiceManagerContext(test.parentPID, test.invocationID, test.rcService, test.svDir, test.s6Logging)
			if got != test.want {
				t.Fatalf("isServiceManagerContext() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestListenTCPAcceptsBracketedIPv6Wildcard(t *testing.T) {
	listener, address, err := listenTCP("[::]", "0")
	require.NoError(t, err)
	require.NotNil(t, listener)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.Contains(t, []string{"::", "0.0.0.0"}, address)
	_, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
}

func TestResumeEncryptionKeyRotationRecoversBeforeConfigSwitch(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, dataDir, t.TempDir())
	t.Setenv("DATABASE_DIR", filepath.Join(dataDir, "db"))
	require.NoError(t, database.InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, dataDir))
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})

	ctx := context.Background()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	oldKeyEncoded := base64.StdEncoding.EncodeToString(oldKey)
	newKeyEncoded := base64.StdEncoding.EncodeToString(newKey)
	credential, err := config.EncryptDNSCredential("dns-api-secret", oldKeyEncoded)
	require.NoError(t, err)

	storedConfig := &config.ServerConfig{}
	storedConfig.Server.Security.EncryptionKey = oldKeyEncoded
	storedConfig.Server.Security.EncryptionKeyVersion = 1
	storedConfig.Server.SSL.LetsEncrypt.DNSCredentials = map[string]string{
		"CF_DNS_API_TOKEN": credential,
	}
	require.NoError(t, config.Save(storedConfig))

	until := time.Now().UTC().Add(time.Hour)
	require.NoError(t, database.StorePendingEncryptionKey(ctx, oldKey, newKey, until))
	require.NoError(t, database.StorePreviousEncryptionKey(ctx, oldKey, newKey, until))

	loadedConfig, err := config.Load(nil)
	require.NoError(t, err)
	require.Equal(t, oldKeyEncoded, loadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", loadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	require.NoError(t, resumeEncryptionKeyRotation(ctx, loadedConfig))
	require.Equal(t, newKeyEncoded, loadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", loadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	reloadedConfig, err := config.Load(nil)
	require.NoError(t, err)
	require.Equal(t, newKeyEncoded, reloadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", reloadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	secretsFile, err := os.ReadFile(filepath.Join(configDir, "secrets.yml"))
	require.NoError(t, err)
	var secrets struct {
		Server struct {
			SSL struct {
				LetsEncrypt struct {
					DNSCredentials map[string]string `yaml:"dns_credentials"`
				} `yaml:"letsencrypt"`
			} `yaml:"ssl"`
		} `yaml:"server"`
	}
	require.NoError(t, yaml.Unmarshal(secretsFile, &secrets))
	sealedCredential := secrets.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"]
	require.Contains(t, sealedCredential, "enc:v1:")
	plain, err := config.DecryptDNSCredential(sealedCredential, newKeyEncoded, "")
	require.NoError(t, err)
	require.Equal(t, "dns-api-secret", plain)
	_, err = config.DecryptDNSCredential(sealedCredential, oldKeyEncoded, "")
	require.Error(t, err)

	retainedPrevious, err := database.LoadPreviousEncryptionKey(ctx, newKey, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, oldKey, retainedPrevious)
	_, _, err = database.LoadPendingEncryptionKey(ctx, oldKey, time.Now().UTC())
	require.ErrorIs(t, err, database.ErrSecretNotFound)
}

func TestRunServesAndShutsDownWithInjectedListener(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	logDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("LOG_DIR", logDir)
	t.Setenv("CACHE_DIR", t.TempDir())
	t.Setenv("BACKUP_DIR", t.TempDir())
	t.Setenv("DATABASE_DIR", filepath.Join(dataDir, "db"))
	paths.SetApplicationPathOverrides(configDir, dataDir, logDir)
	paths.InitCache(t.TempDir())
	paths.InitBackup(t.TempDir())
	cfg, err := config.Load(nil)
	require.NoError(t, err)
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = "0"
	cfg.Server.Database.Driver = "sqlite"
	cfg.Server.Token = "test-only-operator-token"
	cfg.Server.GeoIP.Enabled = false
	cfg.Server.Tor.Expose = false
	cfg.Server.I2P.Enabled = false
	cfg.Server.Notifications.Email.SMTP.Host = "127.0.0.1"
	cfg.Server.Notifications.Email.SMTP.Port = 1

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- run(ctx, cfg, listener, ready) }()

	select {
	case <-ready:
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatal("server did not become ready")
	}
	response, err := http.Get("http://" + listener.Addr().String() + "/server/healthz")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)

	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}
