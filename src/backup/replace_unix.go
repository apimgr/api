//go:build !windows

package backup

import "os"

func replaceBackupFile(from, to string) error {
	return os.Rename(from, to)
}
