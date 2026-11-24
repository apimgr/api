package sysservice

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
)

var ErrAccountToolsUnavailable = errors.New("no supported account-management tools are available")

const (
	// ServiceUserName is the dedicated system account the server drops
	// privileges to after binding privileged ports.
	ServiceUserName = "api"
	// ServiceGroupName matches ServiceUserName; AI.md PART 23 requires the
	// service account's UID and GID to be identical.
	ServiceGroupName = "api"
	// serviceGecos is the account's descriptive comment field.
	serviceGecos = "api service account"
)

// reservedSystemIDs lists well-known UID/GID values that must never be
// assigned to the service account, even when they appear free on the
// current system.
var reservedSystemIDs = buildReservedSystemIDs()

// buildReservedSystemIDs expands the reserved ID ranges from AI.md
// PART 23 (65534 nobody; 980-999 docker/systemd/polkit etc.; 170-179;
// 101-110 sshd/postfix/dovecot etc.) into a lookup set.
func buildReservedSystemIDs() map[int]bool {
	reserved := map[int]bool{65534: true}
	for i := 980; i <= 999; i++ {
		reserved[i] = true
	}
	for i := 170; i <= 179; i++ {
		reserved[i] = true
	}
	for i := 101; i <= 110; i++ {
		reserved[i] = true
	}
	return reserved
}

// systemIDRange returns the (start, floor) bounds to search for a free
// UID/GID, searching downward from start to floor inclusive: Linux/BSD
// 899->200, macOS 399->200.
func systemIDRange() (start, floor int) {
	if runtime.GOOS == "darwin" {
		return 399, 200
	}
	return 899, 200
}

// findAvailableSystemID searches downward from the top of the platform's
// safe UID/GID range, skipping reserved IDs, and returns the first value
// confirmed free in both the passwd and group databases.
func findAvailableSystemID() (int, error) {
	start, floor := systemIDRange()

	for id := start; id >= floor; id-- {
		if reservedSystemIDs[id] {
			continue
		}
		if _, err := user.LookupId(strconv.Itoa(id)); err == nil {
			continue
		}
		if _, err := user.LookupGroupId(strconv.Itoa(id)); err == nil {
			continue
		}
		return id, nil
	}

	return 0, fmt.Errorf("no available system UID/GID found in range %d-%d", floor, start)
}

// CreateServiceUser creates the dedicated "api" system user and group
// (matching UID/GID, nologin shell, no password) with homeDir as its home
// directory, if they do not already exist. It is a no-op on Windows,
// which uses a Virtual Service Account instead of a POSIX user/group.
func CreateServiceUser(homeDir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if _, err := user.Lookup(ServiceUserName); err == nil {
		return nil
	}
	if err := ensureAccountToolsAvailable(); err != nil {
		return err
	}

	if err := os.MkdirAll(homeDir, 0750); err != nil {
		return fmt.Errorf("failed to create home directory %s: %w", homeDir, err)
	}

	id, groupExists, err := resolveServiceIDs()
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		if err := createDarwinServiceUser(id, homeDir, groupExists); err != nil {
			return err
		}
	case "freebsd", "openbsd", "netbsd":
		if err := createBSDServiceUser(id, homeDir, groupExists); err != nil {
			return err
		}
	default:
		if err := createLinuxServiceUser(id, homeDir, groupExists); err != nil {
			return err
		}
	}

	return os.Chown(homeDir, id, id)
}

func ensureAccountToolsAvailable() error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("dscl"); err != nil {
			return fmt.Errorf("dscl is unavailable: %w", ErrAccountToolsUnavailable)
		}
	case "freebsd", "openbsd", "netbsd":
		if _, err := exec.LookPath("pw"); err != nil {
			return fmt.Errorf("pw is unavailable: %w", ErrAccountToolsUnavailable)
		}
	default:
		if _, groupErr := exec.LookPath("groupadd"); groupErr == nil {
			if _, userErr := exec.LookPath("useradd"); userErr == nil {
				return nil
			}
		}
		if _, groupErr := exec.LookPath("addgroup"); groupErr == nil {
			if _, userErr := exec.LookPath("adduser"); userErr == nil {
				return nil
			}
		}
		return fmt.Errorf("useradd/groupadd and BusyBox adduser/addgroup are unavailable: %w", ErrAccountToolsUnavailable)
	}
	return nil
}

// resolveServiceIDs determines the UID/GID to use for the service account.
// A pre-existing "api" group is reused (its GID becomes the UID as well, so
// the matching-ID rule still holds) instead of being recreated; otherwise a
// free ID is searched for in the platform's safe range.
func resolveServiceIDs() (int, bool, error) {
	grp, err := user.LookupGroup(ServiceGroupName)
	if err != nil {
		id, findErr := findAvailableSystemID()
		return id, false, findErr
	}

	gid, convErr := strconv.Atoi(grp.Gid)
	if convErr != nil {
		return 0, true, fmt.Errorf("group %s has a non-numeric gid %q: %w", ServiceGroupName, grp.Gid, convErr)
	}
	start, floor := systemIDRange()
	if gid < floor || gid > start || reservedSystemIDs[gid] {
		return 0, true, fmt.Errorf("group %s has gid %d outside the allowed service-account range %d-%d", ServiceGroupName, gid, floor, start)
	}
	if _, lookupErr := user.LookupId(grp.Gid); lookupErr == nil {
		return 0, true, fmt.Errorf("group %s exists with gid %d but that id is already taken by another user", ServiceGroupName, gid)
	}

	return gid, true, nil
}

// createLinuxServiceUser creates the group then the user via groupadd and
// useradd, matching UID and GID exactly. groupExists skips the groupadd step
// when the group is already present on the host.
func createLinuxServiceUser(id int, homeDir string, groupExists bool) error {
	idStr := strconv.Itoa(id)
	if _, err := exec.LookPath("groupadd"); err == nil {
		if _, userErr := exec.LookPath("useradd"); userErr != nil {
			return fmt.Errorf("useradd is unavailable: %w", ErrAccountToolsUnavailable)
		}
		if !groupExists {
			if err := exec.Command("groupadd", "--system", "--gid", idStr, ServiceGroupName).Run(); err != nil {
				return fmt.Errorf("failed to create group %s: %w", ServiceGroupName, err)
			}
		}
		cmd := exec.Command("useradd", "--system", "--uid", idStr, "--gid", idStr, "--home-dir", homeDir, "--no-create-home", "--shell", nologinShell(), "--comment", serviceGecos, ServiceUserName)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create user %s: %w", ServiceUserName, err)
		}
		return nil
	}

	if _, err := exec.LookPath("addgroup"); err != nil {
		return fmt.Errorf("groupadd and BusyBox addgroup are unavailable: %w", ErrAccountToolsUnavailable)
	}
	if _, err := exec.LookPath("adduser"); err != nil {
		return fmt.Errorf("useradd and BusyBox adduser are unavailable: %w", ErrAccountToolsUnavailable)
	}
	if !groupExists {
		if err := exec.Command("addgroup", "-S", "-g", idStr, ServiceGroupName).Run(); err != nil {
			return fmt.Errorf("failed to create group %s: %w", ServiceGroupName, err)
		}
	}
	cmd := exec.Command("adduser", "-S", "-D", "-H", "-u", idStr, "-G", ServiceGroupName, "-h", homeDir, "-s", nologinShell(), "-g", serviceGecos, ServiceUserName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create user %s: %w", ServiceUserName, err)
	}
	return nil
}

// createDarwinServiceUser creates the group then the user via dscl,
// hidden from the login window with /usr/bin/false as its shell.
// groupExists skips the group creation steps when it is already present.
func createDarwinServiceUser(id int, homeDir string, groupExists bool) error {
	for _, args := range darwinServiceUserSteps(id, homeDir, groupExists) {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("dscl command %v failed: %w", args, err)
		}
	}

	return nil
}

// darwinServiceUserSteps builds the ordered dscl argv list that creates the
// macOS service group and account, matching the AI.md PART 23 sequence.
func darwinServiceUserSteps(id int, homeDir string, groupExists bool) [][]string {
	idStr := strconv.Itoa(id)
	groupPath := "/Groups/" + ServiceGroupName
	userPath := "/Users/" + ServiceUserName

	steps := [][]string{}
	if !groupExists {
		steps = append(steps,
			[]string{"dscl", ".", "-create", groupPath},
			[]string{"dscl", ".", "-create", groupPath, "PrimaryGroupID", idStr},
			[]string{"dscl", ".", "-create", groupPath, "Password", "*"},
		)
	}
	steps = append(steps, [][]string{
		{"dscl", ".", "-create", userPath},
		{"dscl", ".", "-create", userPath, "UniqueID", idStr},
		{"dscl", ".", "-create", userPath, "PrimaryGroupID", idStr},
		{"dscl", ".", "-create", userPath, "UserShell", "/usr/bin/false"},
		{"dscl", ".", "-create", userPath, "NFSHomeDirectory", homeDir},
		{"dscl", ".", "-create", userPath, "RealName", serviceGecos},
		{"dscl", ".", "-create", userPath, "IsHidden", "1"},
		{"dscl", ".", "-create", userPath, "Password", "*"},
	}...)

	return steps
}

// createBSDServiceUser creates the group then the user via pw. groupExists
// skips the groupadd step when the group is already present on the host.
func createBSDServiceUser(id int, homeDir string, groupExists bool) error {
	idStr := strconv.Itoa(id)

	if !groupExists {
		if err := exec.Command("pw", "groupadd", ServiceGroupName, "-g", idStr).Run(); err != nil {
			return fmt.Errorf("failed to create group %s: %w", ServiceGroupName, err)
		}
	}

	cmd := exec.Command("pw", "useradd", ServiceUserName,
		"-u", idStr,
		"-g", idStr,
		"-d", homeDir,
		"-s", nologinShell(),
		"-c", serviceGecos,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create user %s: %w", ServiceUserName, err)
	}

	return nil
}

// nologinShell returns the platform's canonical nologin shell path.
func nologinShell() string {
	for _, candidate := range []string{"/sbin/nologin", "/usr/sbin/nologin"} {
		if fileExists(candidate) {
			return candidate
		}
	}
	return "/sbin/nologin"
}

// DeleteServiceUser removes the dedicated "api" system user and group. It
// is a no-op on Windows and is idempotent - deleting an already-absent
// user/group is not an error.
func DeleteServiceUser() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if _, err := user.Lookup(ServiceUserName); err != nil {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		if err := exec.Command("dscl", ".", "-delete", "/Users/"+ServiceUserName).Run(); err != nil {
			return fmt.Errorf("failed to delete service user %s: %w", ServiceUserName, err)
		}
		if err := deleteServiceGroup("dscl", ".", "-delete", "/Groups/"+ServiceGroupName); err != nil {
			return err
		}
	case "freebsd", "openbsd", "netbsd":
		if err := exec.Command("pw", "userdel", ServiceUserName).Run(); err != nil {
			return fmt.Errorf("failed to delete service user %s: %w", ServiceUserName, err)
		}
		if err := deleteServiceGroup("pw", "groupdel", ServiceGroupName); err != nil {
			return err
		}
	default:
		userDelete := "userdel"
		if _, err := exec.LookPath(userDelete); err != nil {
			userDelete = "deluser"
		}
		if _, err := exec.LookPath(userDelete); err != nil {
			return fmt.Errorf("cannot delete service user %s: %w", ServiceUserName, ErrAccountToolsUnavailable)
		}
		if err := exec.Command(userDelete, ServiceUserName).Run(); err != nil {
			return fmt.Errorf("failed to delete service user %s: %w", ServiceUserName, err)
		}
		groupDelete := "groupdel"
		if _, err := exec.LookPath(groupDelete); err != nil {
			groupDelete = "delgroup"
		}
		if err := deleteServiceGroup(groupDelete, ServiceGroupName); err != nil {
			return err
		}
	}

	return nil
}

// deleteServiceGroup runs the platform's group-deletion command, treating
// an already-absent group as success. Some userdel implementations remove
// the account's primary group along with the user, so the group can be
// gone by the time this runs.
func deleteServiceGroup(name string, args ...string) error {
	if _, err := user.LookupGroup(ServiceGroupName); err != nil {
		return nil
	}
	if err := exec.Command(name, args...).Run(); err != nil {
		return fmt.Errorf("failed to delete group %s: %w", ServiceGroupName, err)
	}
	return nil
}
