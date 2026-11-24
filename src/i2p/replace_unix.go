//go:build !windows

package i2p

import "os"

func atomicReplace(source, destination string) error {
	return os.Rename(source, destination)
}
