package sysservice

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
)

var ErrAccountToolsUnavailable = errors.New("no supported account-management tools are available")

const (
	// ServiceUserName is the dedicated system account the server drops
	// privileges to after binding privileged ports.
	ServiceUserName = "api"
	// ServiceGroupName matches ServiceUserName; newly created accounts use
	// matching UID and GID values as required by AI.md PART 23.
	ServiceGroupName = "api"
	// serviceGecos is the account's descriptive comment field.
	serviceGecos             = "api service account"
	serviceAccountMarkerName = ".api-service-account-created"
)

type serviceAccountManagement struct {
	userCreated  bool
	groupCreated bool
}

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
		} else if !isUnknownUserID(err, id) {
			return 0, fmt.Errorf("check whether UID %d is available: %w", id, err)
		}
		if _, err := user.LookupGroupId(strconv.Itoa(id)); err == nil {
			continue
		} else if !isUnknownGroupID(err, id) {
			return 0, fmt.Errorf("check whether GID %d is available: %w", id, err)
		}
		return id, nil
	}

	return 0, fmt.Errorf("no available system UID/GID found in range %d-%d", floor, start)
}

// CreateServiceUser creates the dedicated "api" system user and group
// (matching UID/GID, nologin shell, no password) with homeDir as its home
// directory, if they do not already exist. A pre-existing account is reused
// only when it meets PART 23's system-account restrictions. It is a no-op on Windows,
// which uses a Virtual Service Account instead of a POSIX user/group.
func CreateServiceUser(homeDir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if existing, err := user.Lookup(ServiceUserName); err == nil {
		shell, err := serviceAccountShell(existing.Username)
		if err != nil {
			return err
		}
		group, err := user.LookupGroup(ServiceGroupName)
		if err != nil {
			return fmt.Errorf("existing account %s conflicts with the service identity: group %s is missing", existing.Username, ServiceGroupName)
		}
		return validateExistingServiceAccount(existing, group, shell)
	} else if !errors.Is(err, user.UnknownUserError(ServiceUserName)) {
		return fmt.Errorf("look up service account %s: %w", ServiceUserName, err)
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

	if err := os.Chown(homeDir, id, id); err != nil {
		return fmt.Errorf("set service account home ownership: %w", err)
	}
	management := serviceAccountManagement{userCreated: true, groupCreated: !groupExists}
	if err := writeServiceAccountManagement(management); err != nil {
		cleanupErr := deleteServiceAccount(management)
		return errors.Join(fmt.Errorf("record managed service account ownership: %w", err), cleanupErr)
	}
	return nil
}

// serviceAccountShell returns the login shell recorded for a local account.
// The standard os/user package intentionally does not expose this passwd field.
func serviceAccountShell(username string) (string, error) {
	if runtime.GOOS == "darwin" {
		output, err := exec.Command("dscl", ".", "-read", "/Users/"+username, "UserShell").Output()
		if err != nil {
			return "", fmt.Errorf("read shell for service account %s: %w", username, err)
		}
		fields := strings.Fields(string(output))
		if len(fields) == 2 && fields[0] == "UserShell:" {
			return fields[1], nil
		}
		return "", fmt.Errorf("could not read shell for service account %s", username)
	}
	if runtime.GOOS == "freebsd" || runtime.GOOS == "openbsd" || runtime.GOOS == "netbsd" {
		output, err := exec.Command("pw", "usershow", "-n", username, "-7").Output()
		if err == nil {
			fields := strings.Split(strings.TrimSpace(string(output)), ":")
			if len(fields) >= 10 && fields[0] == username {
				return fields[len(fields)-1], nil
			}
		}
	}
	contents, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return "", fmt.Errorf("read passwd database for service account %s: %w", username, err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[0] == username {
			return fields[6], nil
		}
	}
	return "", fmt.Errorf("service account %s is not present in /etc/passwd", username)
}

// validateExistingServiceAccount enforces PART 23's narrow reuse exception.
// Newly created accounts use the stricter 200-899 matching-ID allocation rule.
func validateExistingServiceAccount(account *user.User, group *user.Group, shell string) error {
	if account == nil || group == nil {
		return fmt.Errorf("existing service account data is incomplete")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return fmt.Errorf("existing service account %s has invalid uid %q", account.Username, account.Uid)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return fmt.Errorf("existing service account %s has invalid primary gid %q", account.Username, account.Gid)
	}
	if uid < 1 || uid > 999 || gid < 1 || gid > 999 {
		return fmt.Errorf("existing account %s conflicts with the service identity: uid and primary gid must both be from 1 to 999", account.Username)
	}
	if shell != "/sbin/nologin" && shell != "/usr/sbin/nologin" && shell != "/usr/bin/false" && shell != "/bin/false" {
		return fmt.Errorf("existing account %s conflicts with the service identity: shell %q allows login", account.Username, shell)
	}
	if group.Name != ServiceGroupName || group.Gid != account.Gid {
		return fmt.Errorf("existing account %s conflicts with the service identity: primary gid %s does not match group %s gid %s", account.Username, account.Gid, ServiceGroupName, group.Gid)
	}
	return nil
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
		if !errors.Is(err, user.UnknownGroupError(ServiceGroupName)) {
			return 0, false, fmt.Errorf("look up service group %s: %w", ServiceGroupName, err)
		}
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
	} else if !isUnknownUserID(lookupErr, gid) {
		return 0, true, fmt.Errorf("check whether UID %d is available: %w", gid, lookupErr)
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

// DeleteServiceUser removes only the system account and group recorded as
// created by this application. Reused pre-existing accounts are preserved.
// It is a no-op on Windows and when no ownership marker exists.
func DeleteServiceUser() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	management, err := readServiceAccountManagement()
	if err != nil {
		return err
	}
	if !management.userCreated {
		return nil
	}
	if err := deleteServiceAccount(management); err != nil {
		return err
	}
	if err := os.Remove(serviceAccountMarkerPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service account ownership marker: %w", err)
	}
	return nil
}

func deleteServiceAccount(management serviceAccountManagement) error {
	if !management.userCreated {
		return nil
	}
	if _, err := user.Lookup(ServiceUserName); err != nil {
		if errors.Is(err, user.UnknownUserError(ServiceUserName)) {
			return deleteServiceGroupForManagedAccount(management)
		}
		return fmt.Errorf("look up service user %s before deletion: %w", ServiceUserName, err)
	}
	switch runtime.GOOS {
	case "darwin":
		if err := exec.Command("dscl", ".", "-delete", "/Users/"+ServiceUserName).Run(); err != nil {
			return fmt.Errorf("failed to delete service user %s: %w", ServiceUserName, err)
		}
	case "freebsd", "openbsd", "netbsd":
		if err := exec.Command("pw", "userdel", ServiceUserName).Run(); err != nil {
			return fmt.Errorf("failed to delete service user %s: %w", ServiceUserName, err)
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
	}

	return deleteServiceGroupForManagedAccount(management)
}

// deleteServiceGroupForManagedAccount removes the group only when the
// ownership marker says this application created it. An absent group is
// already cleaned up by some user deletion commands and is treated as done.
func deleteServiceGroupForManagedAccount(management serviceAccountManagement) error {
	if !management.groupCreated {
		return nil
	}
	if _, err := user.LookupGroup(ServiceGroupName); err != nil {
		if errors.Is(err, user.UnknownGroupError(ServiceGroupName)) {
			return nil
		}
		return fmt.Errorf("look up service group %s before deletion: %w", ServiceGroupName, err)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("dscl", ".", "-delete", "/Groups/"+ServiceGroupName)
	case "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("pw", "groupdel", ServiceGroupName)
	default:
		groupDelete := "groupdel"
		if _, err := exec.LookPath(groupDelete); err != nil {
			groupDelete = "delgroup"
		}
		cmd = exec.Command(groupDelete, ServiceGroupName)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete group %s: %w", ServiceGroupName, err)
	}
	return nil
}

func serviceAccountMarkerPath() string {
	configDir, _, _ := paths.GetDefaultDirs()
	return filepath.Join(filepath.Dir(configDir), serviceAccountMarkerName)
}

func writeServiceAccountManagement(management serviceAccountManagement) error {
	return writeServiceAccountManagementAt(serviceAccountMarkerPath(), management)
}

func writeServiceAccountManagementAt(path string, management serviceAccountManagement) error {
	content := "user\n"
	if management.groupCreated {
		content += "group\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func readServiceAccountManagement() (serviceAccountManagement, error) {
	return readServiceAccountManagementAt(serviceAccountMarkerPath())
}

func readServiceAccountManagementAt(path string) (serviceAccountManagement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return serviceAccountManagement{}, nil
		}
		return serviceAccountManagement{}, fmt.Errorf("read service account ownership marker: %w", err)
	}
	management := serviceAccountManagement{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		switch line {
		case "user":
			management.userCreated = true
		case "group":
			management.groupCreated = true
		case "":
		default:
			return serviceAccountManagement{}, fmt.Errorf("invalid service account ownership marker")
		}
	}
	if management.groupCreated && !management.userCreated {
		return serviceAccountManagement{}, fmt.Errorf("invalid service account ownership marker")
	}
	return management, nil
}

func isUnknownUserID(err error, id int) bool {
	var unknown user.UnknownUserIdError
	return errors.As(err, &unknown) && unknown == user.UnknownUserIdError(id)
}

func isUnknownGroupID(err error, id int) bool {
	var unknown user.UnknownGroupIdError
	return errors.As(err, &unknown) && unknown == user.UnknownGroupIdError(strconv.Itoa(id))
}
