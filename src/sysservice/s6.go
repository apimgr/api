package sysservice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
)

const s6ServiceBase = "/etc/s6/sv"

func hasRunningS6SVScan() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == "s6-svscan" {
			return true
		}
	}
	return false
}

func s6ScanDir() string {
	if runtime.GOOS == "linux" {
		if entries, err := os.ReadDir("/proc"); err == nil {
			for _, entry := range entries {
				if _, err := strconv.Atoi(entry.Name()); err != nil {
					continue
				}
				base := filepath.Join("/proc", entry.Name())
				comm, err := os.ReadFile(filepath.Join(base, "comm"))
				if err != nil || strings.TrimSpace(string(comm)) != "s6-svscan" {
					continue
				}
				cmdline, err := os.ReadFile(filepath.Join(base, "cmdline"))
				if err != nil {
					continue
				}
				args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
				if scanDir := parseS6ScanDir(args, ""); scanDir != "" {
					return scanDir
				}
			}
		}
	}
	return "/run/service"
}

func parseS6ScanDir(args []string, fallback string) string {
	for i := len(args) - 1; i >= 1; i-- {
		if args[i] == "" || !filepath.IsAbs(args[i]) {
			continue
		}
		info, err := os.Stat(args[i])
		if err == nil && info.IsDir() {
			return args[i]
		}
	}
	return fallback
}

func s6ServiceDefinition() string {
	return filepath.Join(s6ServiceBase, appName)
}

func s6ServiceLink() string {
	return filepath.Join(s6ScanDir(), appName)
}

func s6ServicePath() string {
	return s6ServiceLink()
}

func s6RunScript(binaryPath string) string {
	return "#!/bin/sh\nexec " + shellQuote([]string{binaryPath}) + " 2>&1\n"
}

func s6LogRunScript(logPath string) string {
	return "#!/bin/sh\nexec s6-log n10 s1000000 " + shellQuote([]string{logPath}) + "\n"
}

func installS6() error {
	scanDir := s6ScanDir()
	if !fileExists(scanDir) {
		return fmt.Errorf("s6 scan directory %s is unavailable", scanDir)
	}
	svDir := s6ServiceDefinition()
	if err := os.MkdirAll(svDir, 0755); err != nil {
		return fmt.Errorf("create s6 service directory: %w", err)
	}
	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create runtime directory %s: %w", dir, err)
		}
	}
	binaryPath := GetBinaryPath()
	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("copy server binary for s6: %w", err)
		}
	}
	runScript := s6RunScript(binaryPath)
	if err := os.WriteFile(filepath.Join(svDir, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("write s6 run script: %w", err)
	}
	logDir := filepath.Join(svDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create s6 log service directory: %w", err)
	}
	logScript := s6LogRunScript(paths.LogDir())
	if err := os.WriteFile(filepath.Join(logDir, "run"), []byte(logScript), 0755); err != nil {
		return fmt.Errorf("write s6 log run script: %w", err)
	}
	linkPath := filepath.Join(scanDir, appName)
	if target, err := os.Readlink(linkPath); err == nil {
		if target != svDir {
			return fmt.Errorf("s6 service link %s already points to a different definition", linkPath)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect s6 service link: %w", err)
	} else if err := os.Symlink(svDir, linkPath); err != nil {
		return fmt.Errorf("enable s6 service: %w", err)
	}
	if err := exec.Command("s6-svscanctl", "-a", scanDir).Run(); err != nil {
		return fmt.Errorf("notify s6 service scan directory: %w", err)
	}
	fmt.Printf("s6 service installed at: %s\n", svDir)
	return nil
}

func uninstallS6() error {
	scanDir := s6ScanDir()
	servicePath := filepath.Join(scanDir, appName)
	if _, err := os.Lstat(servicePath); err == nil {
		if err := exec.Command("s6-svc", "-d", servicePath).Run(); err != nil {
			return fmt.Errorf("stop s6 service before uninstall: %w", err)
		}
		if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("disable s6 service: %w", err)
		}
		if err := exec.Command("s6-svscanctl", "-a", scanDir).Run(); err != nil {
			return fmt.Errorf("notify s6 after disabling service: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect s6 service link: %w", err)
	}
	if err := os.RemoveAll(s6ServiceDefinition()); err != nil {
		return fmt.Errorf("remove s6 service definition: %w", err)
	}
	fmt.Println("s6 service uninstalled")
	return nil
}
