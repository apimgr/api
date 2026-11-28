//go:build !windows

package sysservice

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// ServiceAccountIDs returns the UID and primary GID of the dedicated service
// account after validating the PART 23 existing-account reuse restrictions.
// Newly created accounts have matching IDs; a compliant pre-existing system
// account may retain its distinct non-root UID and primary GID.
func ServiceAccountIDs() (uid, gid int, err error) {
	u, err := user.Lookup(ServiceUserName)
	if err != nil {
		return 0, 0, fmt.Errorf("service account %s does not exist: %w", ServiceUserName, err)
	}

	uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("service account %s has a non-numeric uid %q: %w", ServiceUserName, u.Uid, err)
	}
	gid, err = strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("service account %s has a non-numeric gid %q: %w", ServiceUserName, u.Gid, err)
	}
	shell, err := serviceAccountShell(u.Username)
	if err != nil {
		return 0, 0, err
	}
	group, err := user.LookupGroup(ServiceGroupName)
	if err != nil {
		return 0, 0, fmt.Errorf("service account group %s does not exist: %w", ServiceGroupName, err)
	}
	if err := validateExistingServiceAccount(u, group, shell); err != nil {
		return 0, 0, err
	}

	return uid, gid, nil
}

// DropPrivileges permanently drops the process from root to the given
// unprivileged uid/gid, in the only safe order: supplementary groups
// first, then the group, then the user. It verifies the drop actually
// took effect - both the effective and the real ids must have changed -
// and returns an error if the process could still regain root, so callers
// can abort rather than keep serving with full privileges.
//
// Calling it when the process is already unprivileged is a no-op.
func DropPrivileges(uid, gid int) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if uid <= 0 || gid <= 0 {
		return fmt.Errorf("refusing to drop privileges to uid %d/gid %d: not an unprivileged account", uid, gid)
	}

	// Supplementary groups must go first: they cannot be dropped once the
	// process no longer has root, so a later Setgroups would leave the
	// process holding root's group memberships.
	if err := syscall.Setgroups([]int{gid}); err != nil {
		return fmt.Errorf("failed to set supplementary groups to [%d]: %w", gid, err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("failed to drop to gid %d: %w", gid, err)
	}
	// Setuid comes last - dropping the user first would remove the
	// privilege needed to change the group.
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("failed to drop to uid %d: %w", uid, err)
	}

	return verifyPrivilegeDrop(uid, gid)
}

// verifyPrivilegeDrop confirms the process really lost root: the real and
// effective ids must both equal the target, otherwise the process could
// still restore root privileges through setuid(0).
func verifyPrivilegeDrop(uid, gid int) error {
	if os.Geteuid() != uid || os.Getuid() != uid {
		return fmt.Errorf("privilege drop failed: uid is %d/%d, expected %d", os.Getuid(), os.Geteuid(), uid)
	}
	if os.Getegid() != gid || os.Getgid() != gid {
		return fmt.Errorf("privilege drop failed: gid is %d/%d, expected %d", os.Getgid(), os.Getegid(), gid)
	}
	return nil
}
