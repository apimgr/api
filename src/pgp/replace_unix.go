//go:build !windows

package pgp

import "os"

func replaceFileAtomically(source, target string) error {
	return os.Rename(source, target)
}
