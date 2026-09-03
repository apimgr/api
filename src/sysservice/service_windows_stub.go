//go:build !windows

package sysservice

import "fmt"

// installWindows is only implemented when built for GOOS=windows, since it
// depends on golang.org/x/sys/windows/svc/mgr which cannot compile on
// other platforms.
func installWindows() error {
	return fmt.Errorf("windows service installation is only available when built for GOOS=windows")
}

// disableWindows is only implemented when built for GOOS=windows, since it
// depends on golang.org/x/sys/windows/svc/mgr which cannot compile on
// other platforms.
func disableWindows() error {
	return fmt.Errorf("windows service management is only available when built for GOOS=windows")
}

// uninstallWindows is only implemented when built for GOOS=windows, since
// it depends on golang.org/x/sys/windows/svc/mgr which cannot compile on
// other platforms.
func uninstallWindows() error {
	return fmt.Errorf("windows service removal is only available when built for GOOS=windows")
}
