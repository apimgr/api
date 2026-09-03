//go:build windows

package sysservice

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/apimgr/api/src/paths"
)

// installWindows creates (or re-configures) a Windows service running under
// the built-in Virtual Service Account NT SERVICE\api, per AI.md PART 24 -
// never Local System, Administrator, or a logged-in user account. An
// already-installed service is reconfigured rather than treated as an error,
// so --service --install doubles as the documented re-enable path.
func installWindows() error {
	binaryPath := GetBinaryPath()

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	config := mgr.Config{
		DisplayName:      windowsDisplayName,
		Description:      windowsDescription,
		StartType:        mgr.StartAutomatic,
		BinaryPathName:   binaryPath,
		ServiceStartName: windowsServiceAccount,
	}

	if existing, openErr := m.OpenService(appName); openErr == nil {
		defer existing.Close()
		// UpdateConfig replaces the whole record, so start from the service's
		// current config and override only the fields this install owns.
		current, configErr := existing.Config()
		if configErr != nil {
			return fmt.Errorf("failed to read Windows service config: %w", configErr)
		}
		current.DisplayName = config.DisplayName
		current.Description = config.Description
		current.StartType = config.StartType
		current.BinaryPathName = config.BinaryPathName
		current.ServiceStartName = config.ServiceStartName
		if err := existing.UpdateConfig(current); err != nil {
			return fmt.Errorf("failed to reconfigure Windows service: %w", err)
		}
	} else {
		s, createErr := m.CreateService(appName, binaryPath, config)
		if createErr != nil {
			return fmt.Errorf("failed to create Windows service: %w", createErr)
		}
		defer s.Close()
	}

	if err := grantServiceAccountAccess(); err != nil {
		return err
	}

	fmt.Printf("Windows service '%s' installed (running as %s)\n", appName, windowsServiceAccount)
	fmt.Println()
	fmt.Println("To start the service:")
	fmt.Printf("  sc.exe start %s\n", appName)

	return nil
}

// grantServiceAccountAccess gives the Virtual Service Account FullControl
// over the directories the server writes to, since a VSA has no rights to
// them by default. Missing directories are created first so the grant has a
// target.
func grantServiceAccountAccess() error {
	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		grant := fmt.Sprintf("%s:(OI)(CI)F", windowsServiceAccount)
		if err := exec.Command("icacls", dir, "/grant", grant).Run(); err != nil {
			return fmt.Errorf("failed to grant %s access to %s: %w", windowsServiceAccount, dir, err)
		}
	}

	return nil
}

// disableWindows stops the service and sets its start type to disabled,
// leaving the service definition, data, and configuration in place per
// AI.md PART 23's --service --disable semantics.
func disableWindows() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(appName)
	if err != nil {
		return fmt.Errorf("failed to open Windows service: %w", err)
	}
	defer s.Close()

	s.Control(svc.Stop)

	config, err := s.Config()
	if err != nil {
		return fmt.Errorf("failed to read Windows service config: %w", err)
	}
	config.StartType = mgr.StartDisabled

	if err := s.UpdateConfig(config); err != nil {
		return fmt.Errorf("failed to disable Windows service: %w", err)
	}

	return nil
}

// uninstallWindows stops and deletes the Windows service.
func uninstallWindows() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(appName)
	if err != nil {
		return fmt.Errorf("failed to open Windows service: %w", err)
	}
	defer s.Close()

	s.Control(svc.Stop)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("failed to delete Windows service: %w", err)
	}

	fmt.Printf("Windows service '%s' uninstalled\n", appName)
	return nil
}
