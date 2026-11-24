//go:build !windows

package paths

import "os"

func isElevatedNow() bool {
	return os.Geteuid() == 0
}
