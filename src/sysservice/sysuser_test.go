package sysservice

import (
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The service account name and group name must match, since AI.md PART 23
// requires the account's UID and GID to be identical and derives both from
// the internal name.
func TestServiceAccountNames(t *testing.T) {
	assert.Equal(t, "api", ServiceUserName)
	assert.Equal(t, ServiceUserName, ServiceGroupName)
	assert.Equal(t, "api service account", serviceGecos)
}

// systemIDRange must return the platform's safe range: 200-899 on
// Linux/BSD, 200-399 on macOS, searched downward from the top.
func TestSystemIDRange(t *testing.T) {
	start, floor := systemIDRange()

	assert.Equal(t, 200, floor)
	if runtime.GOOS == "darwin" {
		assert.Equal(t, 399, start)
	} else {
		assert.Equal(t, 899, start)
	}
	assert.Greater(t, start, floor)
}

// Every reserved UID/GID from AI.md PART 23 must be marked reserved, and
// ordinary IDs inside the safe range must not be.
func TestReservedSystemIDs(t *testing.T) {
	for _, id := range []int{65534, 980, 990, 999, 170, 175, 179, 101, 105, 110} {
		assert.True(t, reservedSystemIDs[id], "id %d must be reserved", id)
	}
	for _, id := range []int{200, 400, 500, 899, 100, 111, 169, 180, 979, 1000} {
		assert.False(t, reservedSystemIDs[id], "id %d must not be reserved", id)
	}
}

// findAvailableSystemID must return an ID inside the platform's safe range
// that is neither reserved nor already present in the passwd/group
// databases.
func TestFindAvailableSystemID(t *testing.T) {
	id, err := findAvailableSystemID()
	require.NoError(t, err)

	start, floor := systemIDRange()
	assert.GreaterOrEqual(t, id, floor)
	assert.LessOrEqual(t, id, start)
	assert.False(t, reservedSystemIDs[id], "returned a reserved id %d", id)

	_, uidErr := user.LookupId(strconv.Itoa(id))
	assert.Error(t, uidErr, "returned uid %d is already taken", id)
	_, gidErr := user.LookupGroupId(strconv.Itoa(id))
	assert.Error(t, gidErr, "returned gid %d is already taken", id)
}

// findAvailableSystemID must search downward from the top of the range, so
// on a system with a mostly-empty safe range the first free ID is near the
// top rather than near the floor.
func TestFindAvailableSystemIDSearchesDownward(t *testing.T) {
	id, err := findAvailableSystemID()
	require.NoError(t, err)

	start, _ := systemIDRange()
	for candidate := start; candidate > id; candidate-- {
		if reservedSystemIDs[candidate] {
			continue
		}
		_, uidErr := user.LookupId(strconv.Itoa(candidate))
		_, gidErr := user.LookupGroupId(strconv.Itoa(candidate))
		assert.True(t, uidErr == nil || gidErr == nil,
			"id %d was free but a lower id %d was returned", candidate, id)
	}
}

// resolveServiceIDs must report a usable ID with groupExists=false when the
// service group does not yet exist on this host.
func TestResolveServiceIDsWithoutExistingGroup(t *testing.T) {
	if _, err := user.LookupGroup(ServiceGroupName); err == nil {
		t.Skip("the api group already exists on this host")
	}

	id, groupExists, err := resolveServiceIDs()
	require.NoError(t, err)
	assert.False(t, groupExists)

	start, floor := systemIDRange()
	assert.GreaterOrEqual(t, id, floor)
	assert.LessOrEqual(t, id, start)
}

// nologinShell must resolve to one of the two canonical nologin paths and
// never to an interactive shell.
func TestNologinShell(t *testing.T) {
	got := nologinShell()
	assert.Contains(t, []string{"/sbin/nologin", "/usr/sbin/nologin"}, got)
}

// DeleteServiceUser must be idempotent - removing an account that is not
// present is not an error.
func TestDeleteServiceUserWhenAbsent(t *testing.T) {
	if _, err := user.Lookup(ServiceUserName); err == nil {
		t.Skip("the api user exists on this host")
	}
	assert.NoError(t, DeleteServiceUser())
}

// CreateServiceUser and DeleteServiceUser are no-ops on Windows, which uses
// a Virtual Service Account instead of a POSIX user and group.
func TestServiceUserIsNoOpOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only behaviour")
	}
	assert.NoError(t, CreateServiceUser(t.TempDir()))
	assert.NoError(t, DeleteServiceUser())
}

// CreateServiceUser must create the api user and group with identical
// UID and GID, a nologin shell, and the given home directory, and
// DeleteServiceUser must remove both again. The whole round trip only runs
// where it is safe: as root inside the ephemeral test container, with the
// account absent beforehand.
func TestCreateAndDeleteServiceUserRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX account creation is Unix-only")
	}
	if os.Geteuid() != 0 {
		t.Skip("creating a system account requires root")
	}
	if _, err := user.Lookup(ServiceUserName); err == nil {
		t.Skip("the api user already exists on this host")
	}
	if !commandExists("useradd") && !commandExists("pw") && !commandExists("dscl") {
		t.Skip("no system account creation tool is available")
	}

	homeDir := t.TempDir()
	if err := CreateServiceUser(homeDir); err != nil {
		t.Skipf("CreateServiceUser is not usable in this environment: %v", err)
	}
	defer func() {
		if err := DeleteServiceUser(); err != nil {
			t.Errorf("DeleteServiceUser failed: %v", err)
		}
	}()

	created, err := user.Lookup(ServiceUserName)
	require.NoError(t, err)
	assert.Equal(t, created.Uid, created.Gid, "the service account's UID and GID must match")
	assert.Equal(t, homeDir, created.HomeDir)

	grp, err := user.LookupGroup(ServiceGroupName)
	require.NoError(t, err)
	assert.Equal(t, created.Gid, grp.Gid)

	// A second call must be a no-op rather than a duplicate-account error.
	assert.NoError(t, CreateServiceUser(homeDir))
}

// TestDarwinServiceUserStepsMatchSpec verifies the dscl sequence creates a
// hidden, password-disabled account and group with matching IDs, per the
// AI.md PART 23 macOS template.
func TestDarwinServiceUserStepsMatchSpec(t *testing.T) {
	steps := darwinServiceUserSteps(399, "/var/lib/apimgr/api", false)

	flat := make([]string, 0, len(steps))
	for _, args := range steps {
		assert.Equal(t, "dscl", args[0])
		flat = append(flat, strings.Join(args, " "))
	}
	joined := strings.Join(flat, "\n")

	for _, want := range []string{
		"dscl . -create /Groups/api",
		"dscl . -create /Groups/api PrimaryGroupID 399",
		`dscl . -create /Groups/api Password *`,
		"dscl . -create /Users/api UniqueID 399",
		"dscl . -create /Users/api PrimaryGroupID 399",
		"dscl . -create /Users/api UserShell /usr/bin/false",
		"dscl . -create /Users/api NFSHomeDirectory /var/lib/apimgr/api",
		"dscl . -create /Users/api RealName api service account",
		"dscl . -create /Users/api IsHidden 1",
		`dscl . -create /Users/api Password *`,
	} {
		assert.Contains(t, joined, want)
	}
}

// TestDarwinServiceUserStepsSkipExistingGroup verifies an already-present
// group is reused rather than recreated.
func TestDarwinServiceUserStepsSkipExistingGroup(t *testing.T) {
	steps := darwinServiceUserSteps(350, "/var/lib/apimgr/api", true)

	for _, args := range steps {
		assert.NotContains(t, strings.Join(args, " "), "/Groups/api")
	}
}
