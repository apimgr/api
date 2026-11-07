//go:build !windows

package sysservice

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// ServiceAccountIDs returns the UID and GID of the dedicated service
// account. Both values are always identical on a correctly created
// account (AI.md PART 23 "UID/GID Must match"), and a mismatch is
// reported as an error rather than silently accepted.
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
	if uid != gid {
		return 0, 0, fmt.Errorf("service account %s has mismatched uid %d and gid %d; AI.md PART 23 requires them to be identical", ServiceUserName, uid, gid)
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
