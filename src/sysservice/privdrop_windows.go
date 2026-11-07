//go:build windows

package sysservice

import "fmt"

// ServiceAccountIDs has no meaning on Windows: services run under a
// Virtual Service Account (NT SERVICE\api) managed by the service control
// manager, which has no POSIX uid/gid. It always reports that no numeric
// ids exist.
func ServiceAccountIDs() (uid, gid int, err error) {
	return 0, 0, fmt.Errorf("windows services use the virtual service account NT SERVICE\\%s and have no uid/gid", ServiceUserName)
}

// DropPrivileges is a no-op on Windows. The Virtual Service Account is
// already a minimal-privilege isolated identity assigned by the service
// control manager at start, so there is nothing to drop.
func DropPrivileges(uid, gid int) error {
	return nil
}
