//go:build !windows

package sysservice

import (
	"os"
	"os/user"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DropPrivileges must do nothing when the process is already unprivileged,
// so an ordinary user-mode start never fails on it.
func TestDropPrivilegesNoOpWhenUnprivileged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this test covers the unprivileged path")
	}
	assert.NoError(t, DropPrivileges(1000, 1000))
}

// DropPrivileges must refuse a root (or otherwise invalid) target rather
// than "dropping" to a still-privileged account.
func TestDropPrivilegesRejectsPrivilegedTarget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the target check only runs while elevated")
	}
	assert.Error(t, DropPrivileges(0, 0))
	assert.Error(t, DropPrivileges(-1, -1))
}

// verifyPrivilegeDrop must accept the ids the process actually holds and
// reject anything else, since a mismatch means root was never released.
func TestVerifyPrivilegeDrop(t *testing.T) {
	uid := os.Getuid()
	gid := os.Getgid()

	if os.Geteuid() == uid && os.Getegid() == gid {
		assert.NoError(t, verifyPrivilegeDrop(uid, gid))
	}
	assert.Error(t, verifyPrivilegeDrop(uid+1, gid))
	assert.Error(t, verifyPrivilegeDrop(uid, gid+1))
}

// ServiceAccountIDs must report a clear error when the service account has
// not been created yet, instead of returning a zero uid the caller could
// mistake for a valid target.
func TestServiceAccountIDsWhenAbsent(t *testing.T) {
	if _, err := user.Lookup(ServiceUserName); err == nil {
		t.Skip("the api user exists on this host")
	}

	uid, gid, err := ServiceAccountIDs()
	require.Error(t, err)
	assert.Equal(t, 0, uid)
	assert.Equal(t, 0, gid)
}

// When the service account does exist, its UID and GID must be identical
// and match what the passwd database reports (AI.md PART 23).
func TestServiceAccountIDsWhenPresent(t *testing.T) {
	u, err := user.Lookup(ServiceUserName)
	if err != nil {
		t.Skip("the api user does not exist on this host")
	}

	uid, gid, err := ServiceAccountIDs()
	if err != nil {
		t.Skipf("the existing api account is not spec-conformant: %v", err)
	}

	assert.Equal(t, uid, gid)
	assert.Equal(t, u.Uid, strconv.Itoa(uid))
	assert.Equal(t, u.Gid, strconv.Itoa(gid))
}
